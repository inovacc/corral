package corral

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Session is a warm, long-running agent backend. Reusing a Session across turns
// avoids the cold-start + model-warmup cost of spawning a coding-agent CLI per
// call — the process stays alive between Sends.
type Session interface {
	// Send runs one turn on the warm session.
	Send(ctx context.Context, req RunRequest) (RunResult, error)
	// Alive reports whether the underlying process is still usable.
	Alive() bool
	// Close terminates the session and frees its process.
	Close() error
}

// SessionOpener is a Provider that can open warm sessions. Providers that
// implement it are driven through the SessionPool (long-running); providers
// that do not fall back to one-shot Run.
type SessionOpener interface {
	Open(ctx context.Context, agent Agent) (Session, error)
}

// SessionPool keeps one warm Session per agent, reusing it across turns and
// transparently reopening it when it dies. Safe for concurrent use.
type SessionPool struct {
	opener SessionOpener
	mu     sync.Mutex
	live   map[string]Session

	// Reopen policy: Run retries the reopen+Send up to maxAttempts, waiting an
	// exponentially growing backoff (baseBackoff, doubling, capped at
	// maxBackoff) between attempts, so a provider that keeps failing to come
	// back degrades instead of hot-looping.
	maxAttempts int
	baseBackoff time.Duration
	maxBackoff  time.Duration
}

// SessionPoolOption tunes a SessionPool's reopen policy.
type SessionPoolOption func(*SessionPool)

// WithMaxAttempts caps how many reopen+Send attempts Run makes before giving up
// (default 3). Values < 1 are ignored.
func WithMaxAttempts(n int) SessionPoolOption {
	return func(p *SessionPool) {
		if n >= 1 {
			p.maxAttempts = n
		}
	}
}

// WithBackoff sets the base and ceiling of the exponential reopen backoff
// (defaults 100ms base, 2s cap). A non-positive base disables the wait.
func WithBackoff(base, max time.Duration) SessionPoolOption {
	return func(p *SessionPool) {
		p.baseBackoff = base
		p.maxBackoff = max
	}
}

// NewSessionPool builds a pool over an opener with the default reopen policy
// (3 attempts, 100ms→2s exponential backoff), overridable via options.
func NewSessionPool(o SessionOpener, opts ...SessionPoolOption) *SessionPool {
	p := &SessionPool{
		opener:      o,
		live:        make(map[string]Session),
		maxAttempts: 3,
		baseBackoff: 100 * time.Millisecond,
		maxBackoff:  2 * time.Second,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// backoffFor returns the wait before the given retry (retry>=1): base,
// 2*base, 4*base, … capped at maxBackoff. Doubles in a loop to avoid overflow.
func (p *SessionPool) backoffFor(retry int) time.Duration {
	if retry < 1 || p.baseBackoff <= 0 {
		return 0
	}
	d := p.baseBackoff
	for i := 1; i < retry && d < p.maxBackoff; i++ {
		d *= 2
	}
	if p.maxBackoff > 0 && d > p.maxBackoff {
		d = p.maxBackoff
	}
	return d
}

// sleepCtx waits for d unless ctx is cancelled first, returning ctx.Err() on
// cancellation so a backoff never outlives a cancelled run.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// get returns a warm session for the agent, opening one (or replacing a dead
// one) as needed.
func (p *SessionPool) get(ctx context.Context, agent Agent) (Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.live[agent.Name]; ok {
		if s.Alive() {
			return s, nil
		}
		_ = s.Close()
		delete(p.live, agent.Name)
	}
	s, err := p.opener.Open(ctx, agent)
	if err != nil {
		return nil, err
	}
	p.live[agent.Name] = s
	return s, nil
}

// Run executes a turn on a warm session, reopening on failure so a dead process
// never wedges the pool. It retries the reopen+Send up to maxAttempts with
// exponential backoff between attempts (honoring ctx), so a provider that keeps
// failing to come back degrades to a bounded, backed-off failure instead of a
// hot-loop. A ctx cancellation during a backoff or a probe returns promptly.
func (p *SessionPool) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	var lastErr error
	for attempt := 1; attempt <= p.maxAttempts; attempt++ {
		if attempt > 1 {
			if err := sleepCtx(ctx, p.backoffFor(attempt-1)); err != nil {
				return RunResult{}, err
			}
		}
		s, err := p.get(ctx, req.Agent)
		if err != nil {
			lastErr = err
			continue // reopen failed → back off and retry
		}
		res, err := s.Send(ctx, req)
		if err == nil {
			return res, nil
		}
		lastErr = err
		p.drop(req.Agent.Name, s) // dead/failed session → reopen on next attempt
	}
	return RunResult{}, fmt.Errorf("session pool %q: %d attempt(s) exhausted: %w", req.Agent.Name, p.maxAttempts, lastErr)
}

// drop removes a specific session instance from the pool (only if it is still
// the live one) and closes it.
func (p *SessionPool) drop(name string, s Session) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cur, ok := p.live[name]; ok && cur == s {
		_ = cur.Close()
		delete(p.live, name)
	}
}

// Close tears down every warm session.
func (p *SessionPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var firstErr error
	for k, s := range p.live {
		if e := s.Close(); e != nil && firstErr == nil {
			firstErr = e
		}
		delete(p.live, k)
	}
	return firstErr
}

// oneShotSession adapts a one-shot Provider to the Session interface: each Send
// re-execs the provider. It does NOT stay warm — it exists so the pool presents
// a uniform interface for providers without a persistent mode (Codex exec).
// Providers with a real persistent mode return their own warm Session instead.
type oneShotSession struct {
	p Provider
}

func (s oneShotSession) Send(ctx context.Context, req RunRequest) (RunResult, error) {
	return s.p.Run(ctx, req)
}
func (s oneShotSession) Alive() bool  { return true }
func (s oneShotSession) Close() error { return nil }

// OneShot wraps a Provider as a non-warm Session, for provider packages whose
// backend has no persistent mode (e.g. Antigravity off Windows).
func OneShot(p Provider) Session { return oneShotSession{p: p} }
