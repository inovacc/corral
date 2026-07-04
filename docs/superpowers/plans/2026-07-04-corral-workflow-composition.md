# corral Workflow Composition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add sequential / parallel / loop composition over corral's single-turn `Agency.RunAgent`, dependency-free, testable with a fake runner.

**Architecture:** One new file `workflow.go` (package `corral`) with an `AgentRunner` seam (satisfied by `*Agency`), a `Step` type, and `Sequential`/`Parallel`/`Loop` functions. Additive — no change to `Agency`/`Provider`/`RunRequest`/`RunResult`.

**Tech Stack:** Go, stdlib only (`context`, `fmt`, `sync`, `errors`). No adk-go, no new deps.

## Global Constraints
- Module `github.com/inovacc/corral`; package `corral` at repo root.
- **No new dependency** (corral keeps its `conpty`-only budget). adk-go is NOT imported.
- Composition sits ABOVE `RunAgent`; each turn stays a subscription-CLI run.
- Errors: `fmt.Errorf("workflow: …: %w", err)`; sentinel `ErrLoopExhausted`.
- Commits: conventional, NO AI-attribution. Targeted `git add` (untracked `.superpowers/`, `docs/analysis/` exist — never `-A`).
- Consumed existing surface: `Agency.RunAgent(ctx, ag Agent, input string) (RunResult, error)`; `Agent` (struct); `RunResult{Text, Provider}`.

---

## File Structure
- Create `workflow.go` (package `corral`) — `AgentRunner`, `Step`, `Sequential`, `Parallel`, `Loop`, `ErrLoopExhausted`.
- Create `workflow_test.go` — fake-runner table tests + a runnable `Example`.

---

## Task 1: `AgentRunner` seam + `Sequential`

**Files:** Create `workflow.go`, `workflow_test.go`

**Interfaces:**
- Produces: `type AgentRunner interface { RunAgent(ctx context.Context, ag Agent, input string) (RunResult, error) }`; `type Step struct { Agent Agent; Input func(prev RunResult) string }`; `func Sequential(ctx, r AgentRunner, seed string, steps ...Step) ([]RunResult, error)`.

- [ ] **Step 1: Write the failing test**

Create `workflow_test.go`:
```go
package corral

import (
	"context"
	"errors"
	"testing"
)

// fakeRunner scripts RunAgent responses and records inputs, keyed by call order.
type fakeRunner struct {
	replies []RunResult
	errs    []error
	inputs  []string
	agents  []string
	n       int
}

func (f *fakeRunner) RunAgent(_ context.Context, ag Agent, input string) (RunResult, error) {
	f.inputs = append(f.inputs, input)
	f.agents = append(f.agents, ag.Name)
	i := f.n
	f.n++
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	var rr RunResult
	if i < len(f.replies) {
		rr = f.replies[i]
	}
	return rr, err
}

func TestSequential_ThreadsOutputToNextInput(t *testing.T) {
	f := &fakeRunner{replies: []RunResult{{Text: "one"}, {Text: "two"}}}
	res, err := Sequential(context.Background(), f, "seed",
		Step{Agent: Agent{Name: "a"}},
		Step{Agent: Agent{Name: "b"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[1].Text != "two" {
		t.Fatalf("results = %+v, want 2 ending 'two'", res)
	}
	if f.inputs[0] != "seed" {
		t.Errorf("step 0 input = %q, want seed", f.inputs[0])
	}
	if f.inputs[1] != "one" {
		t.Errorf("step 1 input = %q, want prev text 'one'", f.inputs[1])
	}
}

func TestSequential_StopsOnErrorWithPartials(t *testing.T) {
	f := &fakeRunner{replies: []RunResult{{Text: "one"}, {}}, errs: []error{nil, errors.New("boom")}}
	res, err := Sequential(context.Background(), f, "seed",
		Step{Agent: Agent{Name: "a"}}, Step{Agent: Agent{Name: "b"}}, Step{Agent: Agent{Name: "c"}})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(res) != 1 {
		t.Fatalf("want 1 partial result before the failing step, got %d", len(res))
	}
	if f.n != 2 {
		t.Fatalf("want 2 calls (stop after the failing step), got %d", f.n)
	}
}

func TestSequential_StepInputOverride(t *testing.T) {
	f := &fakeRunner{replies: []RunResult{{Text: "one"}, {Text: "two"}}}
	_, err := Sequential(context.Background(), f, "seed",
		Step{Agent: Agent{Name: "a"}},
		Step{Agent: Agent{Name: "b"}, Input: func(prev RunResult) string { return "OVERRIDE:" + prev.Text }},
	)
	if err != nil {
		t.Fatal(err)
	}
	if f.inputs[1] != "OVERRIDE:one" {
		t.Errorf("override input = %q", f.inputs[1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestSequential -v`
Expected: FAIL — `undefined: Sequential` / `AgentRunner` / `Step`.

- [ ] **Step 3: Create `workflow.go`**

```go
package corral

import (
	"context"
	"errors"
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test . -run TestSequential -v && go build ./...`
Expected: PASS; build clean. `gofmt -w workflow.go workflow_test.go`; `gofmt -l` empty.

- [ ] **Step 5: Commit**

```bash
git add workflow.go workflow_test.go
git commit -m "feat(corral): workflow Sequential composition over RunAgent"
```

---

## Task 2: `Parallel` + `Loop`

**Files:** Modify `workflow.go`, `workflow_test.go`

**Interfaces:**
- Consumes: `AgentRunner`, `Step`, `stepInput` (Task 1).
- Produces: `func Parallel(ctx, r AgentRunner, input string, steps ...Step) ([]RunResult, error)`; `func Loop(ctx, r AgentRunner, seed string, step Step, until func(RunResult) bool, maxIters int) ([]RunResult, error)`; `var ErrLoopExhausted = errors.New("workflow: loop exhausted maxIters without until")`.

- [ ] **Step 1: Write the failing tests**

Add to `workflow_test.go`:
```go
func TestParallel_SameInputAllSteps_OrderedResults(t *testing.T) {
	f := &fakeRunner{replies: []RunResult{{Text: "ra"}, {Text: "rb"}}}
	res, err := Parallel(context.Background(), f, "same",
		Step{Agent: Agent{Name: "a"}}, Step{Agent: Agent{Name: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 results, got %d", len(res))
	}
	// Every step saw the same input.
	for _, in := range f.inputs {
		if in != "same" {
			t.Errorf("parallel step input = %q, want 'same'", in)
		}
	}
}

func TestParallel_SurfacesFirstError(t *testing.T) {
	f := &fakeRunner{replies: []RunResult{{}, {Text: "ok"}}, errs: []error{errors.New("boom"), nil}}
	_, err := Parallel(context.Background(), f, "in",
		Step{Agent: Agent{Name: "a"}}, Step{Agent: Agent{Name: "b"}})
	if err == nil {
		t.Fatal("expected error from failing branch")
	}
	if f.n != 2 {
		t.Fatalf("both branches should still run, got %d calls", f.n)
	}
}

func TestLoop_StopsWhenUntilSatisfied(t *testing.T) {
	f := &fakeRunner{replies: []RunResult{{Text: "a"}, {Text: "b"}, {Text: "DONE"}}}
	res, err := Loop(context.Background(), f, "seed", Step{Agent: Agent{Name: "x"}},
		func(r RunResult) bool { return r.Text == "DONE" }, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 || res[2].Text != "DONE" {
		t.Fatalf("loop results = %+v, want 3 ending DONE", res)
	}
	// Each iteration re-fed the prior output.
	if f.inputs[0] != "seed" || f.inputs[1] != "a" || f.inputs[2] != "b" {
		t.Fatalf("loop inputs = %v", f.inputs)
	}
}

func TestLoop_ExhaustsAtMaxIters(t *testing.T) {
	f := &fakeRunner{replies: []RunResult{{Text: "x"}, {Text: "x"}}}
	res, err := Loop(context.Background(), f, "seed", Step{Agent: Agent{Name: "x"}},
		func(r RunResult) bool { return false }, 2)
	if !errors.Is(err, ErrLoopExhausted) {
		t.Fatalf("want ErrLoopExhausted, got %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 iteration results, got %d", len(res))
	}
}

func TestLoop_NonPositiveMaxItersErrors(t *testing.T) {
	f := &fakeRunner{}
	if _, err := Loop(context.Background(), f, "s", Step{Agent: Agent{Name: "x"}}, func(RunResult) bool { return true }, 0); err == nil {
		t.Fatal("maxIters<=0 must error immediately")
	}
	if f.n != 0 {
		t.Fatalf("no runs on invalid maxIters, got %d", f.n)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test . -run 'TestParallel|TestLoop' -v`
Expected: FAIL — `undefined: Parallel` / `Loop` / `ErrLoopExhausted`.

- [ ] **Step 3: Implement `Parallel` + `Loop`**

Add to `workflow.go`:
```go
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
```
Add `"sync"` to the import block.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test . -run 'TestParallel|TestLoop' -v && go test . -run TestSequential && go build ./...`
Expected: all PASS; build clean. `gofmt -l workflow.go workflow_test.go` empty.

- [ ] **Step 5: Add a runnable Example + commit**

Add to `workflow_test.go`:
```go
func ExampleSequential() {
	f := &fakeRunner{replies: []RunResult{{Text: "draft"}, {Text: "reviewed"}}}
	res, _ := Sequential(context.Background(), f, "topic",
		Step{Agent: Agent{Name: "writer"}},
		Step{Agent: Agent{Name: "reviewer"}},
	)
	fmt.Println(res[len(res)-1].Text)
	// Output: reviewed
}
```
Ensure `fmt` is imported in the test file. Then:
```bash
git add workflow.go workflow_test.go
git commit -m "feat(corral): workflow Parallel + Loop composition"
```

---

## Self-Review Notes
- **Spec coverage:** §2.1 AgentRunner + §2.2 Step + §2.3 Sequential → Task 1; Parallel + Loop → Task 2; §3 semantics (override, stop-on-error, same-input, re-feed, maxIters, ErrLoopExhausted) → both tasks' tests; §5 fake-runner + Example → both tasks.
- **Type consistency:** `stepInput` helper (Task 1) reused by `Sequential` and `Loop`; `AgentRunner`/`Step` defined Task 1, used throughout.
- **Non-goals honored:** no adk-go, no Agency change, no state/events/toolsets.
