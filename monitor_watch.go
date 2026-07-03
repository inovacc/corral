package corral

import (
	"context"
	"sync"
	"time"
)

// Alert is delivered when a monitored provider's worst usage window crosses the
// configured safe threshold. It carries the snapshot so the host can react —
// log it, raise a desktop notification, POST a webhook, or pause the fleet.
type Alert struct {
	Provider  string
	Worst     float64 // worst window UsedPercent (0..100) at fire time
	Threshold float64
	Status    *LimitStatus
	At        time.Time
}

// Sample is one recorded usage poll for a provider — the per-provider trail the
// Monitor keeps for statistics. Status is nil when the poll failed (Err set).
type Sample struct {
	Provider string
	Worst    float64
	Status   *LimitStatus
	Err      string
	At       time.Time
}

type watched struct {
	name string
	r    UsageReporter
}

// Monitor continuously polls subscription providers, records their usage
// snapshots, and fires an Alert when any provider's worst window reaches a safe
// threshold. It is the continuous complement to the per-run checkLimit gate:
// checkLimit withholds a single turn, while Monitor watches the whole fleet over
// time and warns the host before a limit is reached — the "/usage + alert"
// behaviour of the coding-agent CLIs, generalised across providers.
//
// The zero value is not usable; construct with NewMonitor. Poll is safe to call
// directly (tests, on-demand refresh); Run drives it on the interval.
type Monitor struct {
	interval  time.Duration
	threshold float64
	history   int
	onAlert   func(Alert)
	now       func() time.Time

	mu      sync.Mutex
	watch   []watched
	latest  map[string]Sample
	samples map[string][]Sample
	firing  map[string]bool // debounce: currently over-threshold (already alerted)
}

// MonitorOption configures a Monitor.
type MonitorOption func(*Monitor)

// WithInterval sets the poll interval (default 60s; non-positive ignored).
func WithInterval(d time.Duration) MonitorOption {
	return func(m *Monitor) {
		if d > 0 {
			m.interval = d
		}
	}
}

// WithThreshold sets the safe-usage alert threshold as a percent in 0..100
// (default 80). An Alert fires when a provider's worst window reaches it.
// A threshold <= 0 disables alerting (polling/stats still run).
func WithThreshold(pct float64) MonitorOption { return func(m *Monitor) { m.threshold = pct } }

// WithHistory caps the samples kept per provider (default 256; non-positive
// keeps the default).
func WithHistory(n int) MonitorOption {
	return func(m *Monitor) {
		if n > 0 {
			m.history = n
		}
	}
}

// OnAlert registers the callback fired once each time a provider crosses the
// threshold — debounced until the provider recovers below it. The host wires it
// to whatever it wants (slog, a toast, a webhook, pausing dispatch).
func OnAlert(f func(Alert)) MonitorOption { return func(m *Monitor) { m.onAlert = f } }

// NewMonitor builds a Monitor. Register providers with Watch or WatchProvider,
// then call Run (blocking, ctx-bounded) in a goroutine, or Poll on demand.
func NewMonitor(opts ...MonitorOption) *Monitor {
	m := &Monitor{
		interval:  60 * time.Second,
		threshold: 80,
		history:   256,
		now:       time.Now,
		latest:    map[string]Sample{},
		samples:   map[string][]Sample{},
		firing:    map[string]bool{},
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Watch registers a named UsageReporter to poll. A nil reporter is ignored.
func (m *Monitor) Watch(name string, r UsageReporter) {
	if r == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watch = append(m.watch, watched{name: name, r: r})
}

// WatchProvider registers p when it implements UsageReporter, returning whether
// it was watched. Providers without a queryable limit are silently skipped.
func (m *Monitor) WatchProvider(p Provider) bool {
	r, ok := p.(UsageReporter)
	if !ok {
		return false
	}
	m.Watch(p.Name(), r)
	return true
}

// Poll runs one usage round over every watched provider: query it, record a
// sample, and fire an Alert on a fresh threshold crossing.
func (m *Monitor) Poll(ctx context.Context) {
	m.mu.Lock()
	ws := make([]watched, len(m.watch))
	copy(ws, m.watch)
	m.mu.Unlock()

	for _, w := range ws {
		if ctx.Err() != nil {
			return
		}
		s, err := w.r.Usage()
		smp := Sample{Provider: w.name, At: m.now()}
		switch {
		case err != nil:
			smp.Err = err.Error()
		case s != nil:
			smp.Status = s
			smp.Worst = s.Worst()
		}
		m.record(smp)
		m.maybeAlert(w.name, smp)
	}
}

func (m *Monitor) record(s Sample) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latest[s.Provider] = s
	h := append(m.samples[s.Provider], s)
	if len(h) > m.history {
		h = h[len(h)-m.history:]
	}
	m.samples[s.Provider] = h
}

func (m *Monitor) maybeAlert(name string, s Sample) {
	if m.threshold <= 0 || s.Status == nil {
		return
	}
	over := s.Worst >= m.threshold
	m.mu.Lock()
	was := m.firing[name]
	m.firing[name] = over
	cb := m.onAlert
	m.mu.Unlock()
	if over && !was && cb != nil {
		cb(Alert{Provider: name, Worst: s.Worst, Threshold: m.threshold, Status: s.Status, At: s.At})
	}
}

// Run polls immediately, then on the interval until ctx is cancelled. It blocks;
// run it in a goroutine.
func (m *Monitor) Run(ctx context.Context) {
	m.Poll(ctx)
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Poll(ctx)
		}
	}
}

// Latest returns the most recent sample per provider.
func (m *Monitor) Latest() map[string]Sample {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Sample, len(m.latest))
	for k, v := range m.latest {
		out[k] = v
	}
	return out
}

// Samples returns the recorded history for a provider, oldest first.
func (m *Monitor) Samples(name string) []Sample {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.samples[name]
	out := make([]Sample, len(h))
	copy(out, h)
	return out
}
