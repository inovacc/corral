package corral

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// AgentRunner is the single-turn seam workflows drive. *Agency satisfies it via
// its RunAgent method, so workflows compose real agent turns; tests use a fake.
type AgentRunner interface {
	RunAgent(ctx context.Context, ag Agent, input string) (RunResult, error)
}

// Step is one agent turn in a workflow. Input derives this step's input from the
// previous result; nil means "use the previous step's RunResult.Text" (the
// workflow seed for the first step).
type Step struct {
	Agent Agent
	Input func(prev RunResult) string
}

func stepInput(s Step, prev RunResult, seed string, first bool) string {
	if s.Input != nil {
		return s.Input(prev)
	}
	if first {
		return seed
	}
	return prev.Text
}

// Sequential runs steps in order, threading each RunResult.Text into the next
// step's input (unless Step.Input overrides). Stops on the first error,
// returning the results gathered so far plus the wrapped error.
func Sequential(ctx context.Context, r AgentRunner, seed string, steps ...Step) ([]RunResult, error) {
	out := make([]RunResult, 0, len(steps))
	var prev RunResult
	for i, s := range steps {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		in := stepInput(s, prev, seed, i == 0)
		res, err := r.RunAgent(ctx, s.Agent, in)
		if err != nil {
			return out, fmt.Errorf("workflow: step %d (%s): %w", i, s.Agent.Name, err)
		}
		out = append(out, res)
		prev = res
	}
	return out, nil
}

// ErrLoopExhausted is returned by Loop when maxIters is reached without until.
var ErrLoopExhausted = errors.New("workflow: loop exhausted maxIters without until")

// Parallel runs every step concurrently against the same input, returning
// results in step order. All goroutines complete; the first non-nil error is
// returned. Caveat: SessionPool keys one warm session per agent.Name, so
// parallel branches sharing an agent name serialize — use distinct names.
func Parallel(ctx context.Context, r AgentRunner, input string, steps ...Step) ([]RunResult, error) {
	out := make([]RunResult, len(steps))
	errs := make([]error, len(steps))
	var wg sync.WaitGroup
	for i, s := range steps {
		wg.Add(1)
		go func(i int, s Step) {
			defer wg.Done()
			res, err := r.RunAgent(ctx, s.Agent, input)
			out[i] = res
			if err != nil {
				errs[i] = fmt.Errorf("workflow: parallel step %d (%s): %w", i, s.Agent.Name, err)
			}
		}(i, s)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return out, e
		}
	}
	return out, nil
}

// Loop runs step repeatedly, feeding each RunResult back as the next input,
// until until(result) is true or maxIters is reached. maxIters<=0 is an error
// (an unbounded loop is never allowed). Returns every iteration's result;
// returns ErrLoopExhausted if the cap is hit without until succeeding.
func Loop(ctx context.Context, r AgentRunner, seed string, step Step, until func(RunResult) bool, maxIters int) ([]RunResult, error) {
	if maxIters <= 0 {
		return nil, fmt.Errorf("workflow: loop maxIters must be > 0, got %d", maxIters)
	}
	out := make([]RunResult, 0, maxIters)
	var prev RunResult
	for i := 0; i < maxIters; i++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		in := stepInput(step, prev, seed, i == 0)
		res, err := r.RunAgent(ctx, step.Agent, in)
		if err != nil {
			return out, fmt.Errorf("workflow: loop iter %d (%s): %w", i, step.Agent.Name, err)
		}
		out = append(out, res)
		prev = res
		if until != nil && until(res) {
			return out, nil
		}
	}
	return out, ErrLoopExhausted
}
