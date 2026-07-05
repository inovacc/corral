package corral

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// LimitWindow is one subscription rate-limit window a provider exposes for
// monitoring — vendor-neutral so the core can reason about Codex, Claude, or
// Antigravity limits uniformly. Mirrors what a coding agent's `/status` shows.
type LimitWindow struct {
	Name        string    // "5h" | "weekly" | provider-specific label
	UsedPercent float64   // 0..100
	ResetsAt    time.Time // when the window rolls over (zero if unknown)
}

// LeftPercent is the remaining headroom in the window.
func (w LimitWindow) LeftPercent() float64 { return 100 - w.UsedPercent }

// LimitStatus is a provider's current rate-limit snapshot across its windows.
type LimitStatus struct {
	Plan    string
	Windows []LimitWindow
	Source  string // where the snapshot came from (e.g. a session rollout file)
}

// Worst returns the highest used-percent across all windows — the binding
// constraint for deciding whether to keep dispatching work.
func (s *LimitStatus) Worst() float64 {
	var w float64
	for _, win := range s.Windows {
		if win.UsedPercent > w {
			w = win.UsedPercent
		}
	}
	return w
}

// Exhausted reports whether any window is fully spent.
func (s *LimitStatus) Exhausted() bool { return s.Worst() >= 100 }

// OK reports whether the worst window is still under threshold (a percent in
// 0..100). A threshold <= 0 disables the check (always OK).
func (s *LimitStatus) OK(threshold float64) bool {
	return threshold <= 0 || s.Worst() < threshold
}

// String renders the snapshot compactly for logs and the monitor error.
func (s *LimitStatus) String() string {
	var b strings.Builder
	if s.Plan != "" {
		fmt.Fprintf(&b, "plan=%s ", s.Plan)
	}
	for i, w := range s.Windows {
		if i > 0 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "%s=%.0f%%used", w.Name, w.UsedPercent)
		if !w.ResetsAt.IsZero() {
			fmt.Fprintf(&b, "(resets %s)", w.ResetsAt.Format("Mon 02 Jan 15:04"))
		}
	}
	return strings.TrimSpace(b.String())
}

// UsageReporter is an OPTIONAL Provider capability: report current subscription
// rate-limit usage so the Agency can pause a long-running fleet before it walks
// into a hard limit mid-run (rather than failing the turn). Providers without a
// queryable limit (Antigravity, Claude Code) simply do not implement it.
type UsageReporter interface {
	Usage(ctx context.Context) (*LimitStatus, error)
}

// ErrRateLimited is returned by Agency.RunAgent when the provider's usage has
// reached the configured limit threshold and the run was withheld.
var ErrRateLimited = errors.New("agents: rate-limit threshold reached")

// ProviderUsage reads a provider's limit snapshot when it supports monitoring,
// returning (nil, false, nil) when the provider has no queryable limit.
func ProviderUsage(ctx context.Context, p Provider) (status *LimitStatus, supported bool, err error) {
	r, ok := p.(UsageReporter)
	if !ok {
		return nil, false, nil
	}
	s, err := r.Usage(ctx)
	return s, true, err
}

// checkLimit is the Agency's pre-run gate: it withholds a turn when the
// provider reports usage at/over threshold. A missing snapshot or a provider
// without UsageReporter is never blocking — monitoring only ever stops work on
// a positive over-limit signal, never on absence of data.
func checkLimit(ctx context.Context, p Provider, threshold float64) error {
	if threshold <= 0 {
		return nil
	}
	s, supported, err := ProviderUsage(ctx, p)
	return overLimit(s, supported, err, threshold)
}

// overLimit applies the threshold gate to a usage snapshot. A missing snapshot,
// an unsupported provider, or a probe error is never blocking — only a positive
// over-threshold reading withholds the turn.
func overLimit(s *LimitStatus, supported bool, err error, threshold float64) error {
	if threshold <= 0 || !supported || err != nil || s == nil {
		return nil
	}
	if !s.OK(threshold) {
		return fmt.Errorf("%w: %s (threshold %.0f%%)", ErrRateLimited, s.String(), threshold)
	}
	return nil
}

// usageGate coalesces concurrent usage probes so a fleet of N concurrent turns
// fires at most one provider quota call at a time; callers that arrive while a
// probe is in flight share its result instead of stampeding the endpoint. The
// zero value is ready to use. (Pure-stdlib single-flight — the one-dep thesis
// rules out golang.org/x/sync/singleflight.)
type usageGate struct {
	mu       sync.Mutex
	inflight *usageProbe
}

type usageProbe struct {
	done      chan struct{}
	s         *LimitStatus
	supported bool
	err       error
}

// probe returns the provider's usage snapshot, coalescing concurrent callers
// onto a single in-flight ProviderUsage call. The in-flight call runs under the
// first caller's ctx; joiners share its result (a ctx error degrades
// non-blocking for all, per the monitoring philosophy).
func (g *usageGate) probe(ctx context.Context, p Provider) (*LimitStatus, bool, error) {
	g.mu.Lock()
	if pr := g.inflight; pr != nil {
		g.mu.Unlock()
		<-pr.done
		return pr.s, pr.supported, pr.err
	}
	pr := &usageProbe{done: make(chan struct{})}
	g.inflight = pr
	g.mu.Unlock()

	pr.s, pr.supported, pr.err = ProviderUsage(ctx, p)

	g.mu.Lock()
	g.inflight = nil
	g.mu.Unlock()
	close(pr.done)
	return pr.s, pr.supported, pr.err
}
