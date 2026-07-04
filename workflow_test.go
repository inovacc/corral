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
