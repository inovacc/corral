package corral

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubProvider is a Provider that optionally reports usage.
type stubProvider struct {
	name   string
	status *LimitStatus
	err    error
	ran    bool
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) Run(_ context.Context, _ RunRequest) (RunResult, error) {
	s.ran = true
	return RunResult{Text: "ok", Provider: s.name}, nil
}

// usageStub adds the UsageReporter capability.
type usageStub struct{ *stubProvider }

func (u usageStub) Usage(context.Context) (*LimitStatus, error) { return u.status, u.err }

// ctxUsageStub blocks in Usage until the caller's ctx is cancelled, then reports
// the ctx error — modeling a hung provider HTTP call. It proves checkLimit
// threads the caller's ctx all the way into Usage (item #5).
type ctxUsageStub struct {
	*stubProvider
	observed chan struct{}
}

func (c ctxUsageStub) Usage(ctx context.Context) (*LimitStatus, error) {
	<-ctx.Done()
	close(c.observed)
	return nil, ctx.Err()
}

func TestCheckLimit_HonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled — Usage must return promptly, not hang forever
	c := ctxUsageStub{stubProvider: &stubProvider{name: "hang"}, observed: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- checkLimit(ctx, c, 80) }()
	select {
	case err := <-done:
		// A ctx error during Usage must degrade to non-blocking (absence of data
		// never blocks the fleet — only a positive over-limit signal does).
		if err != nil {
			t.Fatalf("ctx-cancelled Usage must degrade non-blocking, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("checkLimit hung on a cancelled ctx — ctx is not threaded through to Usage")
	}
	select {
	case <-c.observed:
	default:
		t.Fatal("Usage never observed ctx cancellation")
	}
}

func TestLimitStatusWorstAndOK(t *testing.T) {
	s := &LimitStatus{Windows: []LimitWindow{{Name: "5h", UsedPercent: 40}, {Name: "weekly", UsedPercent: 96}}}
	if s.Worst() != 96 {
		t.Errorf("worst = %v, want 96", s.Worst())
	}
	if s.Exhausted() {
		t.Error("96%% is not exhausted")
	}
	if s.OK(95) {
		t.Error("96%% should fail a 95%% threshold")
	}
	if !s.OK(98) {
		t.Error("96%% should pass a 98%% threshold")
	}
	if !s.OK(0) {
		t.Error("threshold 0 disables the gate")
	}
}

func TestProviderUsageUnsupported(t *testing.T) {
	_, supported, err := ProviderUsage(context.Background(), &stubProvider{name: "bare"})
	if supported || err != nil {
		t.Errorf("bare provider: supported=%v err=%v", supported, err)
	}
}

func TestAgencyGatesOnLimit(t *testing.T) {
	over := usageStub{&stubProvider{name: "codex", status: &LimitStatus{
		Plan: "plus", Windows: []LimitWindow{{Name: "weekly", UsedPercent: 99}}},
	}}
	a := &Agency{Provider: over, Dir: ".", LimitThreshold: 98}
	_, err := a.RunAgent(context.Background(), Agent{Name: "probe"}, "")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
	if over.ran {
		t.Error("provider ran despite being over the limit")
	}
}

func TestAgencyRunsUnderLimit(t *testing.T) {
	under := usageStub{&stubProvider{name: "codex", status: &LimitStatus{
		Windows: []LimitWindow{{Name: "weekly", UsedPercent: 50}}},
	}}
	a := &Agency{Provider: under, Dir: ".", LimitThreshold: 98}
	res, err := a.RunAgent(context.Background(), Agent{Name: "probe"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "ok" || !under.ran {
		t.Error("provider should have run under the limit")
	}
}

func TestAgencyMissingSnapshotNeverBlocks(t *testing.T) {
	// UsageReporter present but no data (nil status) must not block work.
	none := usageStub{&stubProvider{name: "codex", status: nil}}
	a := &Agency{Provider: none, Dir: ".", LimitThreshold: 98}
	if _, err := a.RunAgent(context.Background(), Agent{Name: "probe"}, ""); err != nil {
		t.Fatalf("missing snapshot must not block: %v", err)
	}
	if !none.ran {
		t.Error("provider should have run when snapshot absent")
	}
}
