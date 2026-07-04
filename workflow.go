package corral

import (
	"context"
	"fmt"
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
