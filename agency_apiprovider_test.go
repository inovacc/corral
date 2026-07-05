package corral

import (
	"context"
	"testing"
)

// apiLikeProvider is a one-shot Provider (NOT a SessionOpener), modeling an API
// backend.
type apiLikeProvider struct{ ran bool }

func (a *apiLikeProvider) Name() string { return "api-like" }
func (a *apiLikeProvider) Run(_ context.Context, _ RunRequest) (RunResult, error) {
	a.ran = true
	return RunResult{Text: "ok", Provider: "api-like"}, nil
}

func TestNewAgencyWithProvider_OneShotNoPool(t *testing.T) {
	p := &apiLikeProvider{}
	a := NewAgencyWithProvider(p, ".")
	if a.Provider != p {
		t.Fatal("provider not set")
	}
	if a.pool != nil {
		t.Error("one-shot provider must not get a session pool")
	}
	if a.LimitThreshold != DefaultLimitThreshold {
		t.Errorf("threshold = %v, want default %v", a.LimitThreshold, DefaultLimitThreshold)
	}
	res, err := a.Run(context.Background(), "quality", "in")
	// unknown agent name -> error, but provider path is what we assert via RunAgent:
	_ = res
	_ = err
	r2, err := a.RunAgent(context.Background(), Agent{Name: "x"}, "in")
	if err != nil || r2.Text != "ok" || !p.ran {
		t.Errorf("RunAgent via explicit provider: res=%+v err=%v ran=%v", r2, err, p.ran)
	}
}
