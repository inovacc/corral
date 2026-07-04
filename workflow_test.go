package corral

import (
	"context"
	"errors"
	"fmt"
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

func ExampleSequential() {
	f := &fakeRunner{replies: []RunResult{{Text: "draft"}, {Text: "reviewed"}}}
	res, _ := Sequential(context.Background(), f, "topic",
		Step{Agent: Agent{Name: "writer"}},
		Step{Agent: Agent{Name: "reviewer"}},
	)
	fmt.Println(res[len(res)-1].Text)
	// Output: reviewed
}
