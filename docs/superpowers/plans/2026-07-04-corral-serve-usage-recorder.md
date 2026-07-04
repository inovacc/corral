# `corral serve` — Usage Recorder Daemon — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `corral serve` daemon that runs the existing `Monitor` unattended and appends provider usage *changes* to a JSONL file.

**Architecture:** Reuse `Monitor` wholesale. Add one generic seam (`Monitor.OnSample`, mirroring `OnAlert`), a pluggable `UsageSink` with one `JSONLSink` implementation that dedups per-provider, and a `serve` command wired into the existing light-command path in `cmd/corral/main.go`.

**Tech Stack:** Go 1.25, Cobra, `log/slog`, stdlib `encoding/json` + `os/signal`. No new dependencies.

## Global Constraints

- **Module:** `github.com/inovacc/corral`; package `corral` at repo root (`D:\weaver-sync\development\personal\projects\agents`).
- **No new dependencies** — stdlib + cobra + the existing corral types only.
- **Logging:** `log/slog` to **stderr** only; stdout/TTY stays clean (the command runs on the mantle-bootstrap-bypass path).
- **Recorder only** — no HTTP/gRPC endpoint, no log rotation.
- **Errors:** `fmt.Errorf("…: %w", err)`; sink write errors are logged and NON-fatal (the daemon keeps running); a startup `--out` open failure IS fatal.
- **Tests:** network-free; `go test ./...`. Reuse the existing `fakeReporter` (a `UsageReporter`) in `monitor_watch_test.go` for Monitor tests.
- **Commits:** conventional; NO AI-attribution / `Co-Authored-By` trailer.
- **Existing types (consume, do not modify except Task 1):**
  - `Sample{Provider string; Worst float64; Status *LimitStatus; Err string; At time.Time}` (`monitor_watch.go`)
  - `Alert{Provider string; Worst float64; Threshold float64; Status *LimitStatus; At time.Time}` (`monitor_watch.go`)
  - `LimitStatus{Plan string; Windows []LimitWindow; Source string}`; `func (s *LimitStatus) Worst() float64` (`monitor.go`)
  - `LimitWindow{Name string; UsedPercent float64; ResetsAt time.Time}` (`monitor.go`)
  - `UsageReporter interface { Usage() (*LimitStatus, error) }` (`monitor.go`)
  - `func (m *Monitor) Watch(name string, r UsageReporter)`; `NewMonitor(opts ...MonitorOption)`; `WithInterval`/`WithThreshold`/`OnAlert`; `Poll(ctx)`; `Run(ctx)` (`monitor_watch.go`)
  - `func ProviderByName(name string) (Provider, error)`; `Provider interface { Name() string; ... }` (`providers.go`/`provider.go`)
  - `func canonicalProviders() []string`; `func homePath(sub ...string) string` (`cmd/corral/usage.go`, `cmd/corral/launch.go` — package `main`)

---

## File Structure

- **Modify** `monitor_watch.go` — add `onSample` field, `OnSample` option, `emitSample`, and one call in `Poll`.
- **Modify** `monitor_watch_test.go` — add `TestMonitorOnSample` (reuses `fakeReporter`).
- **Create** `sink.go` (package `corral`) — `UsageSink` interface + `JSONLSink`.
- **Create** `sink_test.go` (package `corral`) — sink table tests.
- **Create** `cmd/corral/serve.go` (package `main`) — `newServeCmd`, `runServe`, provider resolution.
- **Create** `cmd/corral/serve_test.go` (package `main`) — `runServe --once` smoke.
- **Modify** `cmd/corral/main.go` — add `"serve"` to `isLightCommand`; register `newServeCmd()` in both root command sets.

---

## Task 1: `Monitor.OnSample` seam

**Files:**
- Modify: `monitor_watch.go`
- Test: `monitor_watch_test.go`

**Interfaces:**
- Consumes: existing `Monitor`, `Sample`, `NewMonitor`, `Watch`, `Poll`, the `fakeReporter` test fake.
- Produces: `func OnSample(f func(Sample)) MonitorOption` — registers a callback invoked once per provider per poll (after `record`/`maybeAlert`).

- [ ] **Step 1: Write the failing test**

Add to `monitor_watch_test.go` (reuses the existing `fakeReporter`):

```go
func TestMonitorOnSample(t *testing.T) {
	r := &fakeReporter{used: 42}
	var got []Sample
	m := NewMonitor(WithThreshold(80), OnSample(func(s Sample) { got = append(got, s) }))
	m.Watch("acme", r)

	m.Poll(context.Background())
	if len(got) != 1 {
		t.Fatalf("OnSample calls after 1 poll = %d, want 1", len(got))
	}
	if got[0].Provider != "acme" || got[0].Worst != 42 {
		t.Fatalf("sample = %+v, want provider=acme worst=42", got[0])
	}

	r.used = 55
	m.Poll(context.Background())
	if len(got) != 2 || got[1].Worst != 55 {
		t.Fatalf("after 2nd poll: %d samples, last=%+v, want 2 ending at 55", len(got), got[len(got)-1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestMonitorOnSample -v`
Expected: FAIL — `undefined: OnSample`.

- [ ] **Step 3: Add the field, option, emit method, and Poll call**

In `monitor_watch.go`, add `onSample func(Sample)` to the `Monitor` struct (next to `onAlert func(Alert)`):

```go
	onAlert   func(Alert)
	onSample  func(Sample)
```

Add the option near `OnAlert`:

```go
// OnSample registers a callback fired once per provider on every poll (after the
// sample is recorded and any alert is evaluated). The host wires it to a sink;
// the Monitor stays sink-agnostic.
func OnSample(f func(Sample)) MonitorOption { return func(m *Monitor) { m.onSample = f } }
```

Add the emit helper (mirrors `maybeAlert`'s locking):

```go
func (m *Monitor) emitSample(s Sample) {
	m.mu.Lock()
	cb := m.onSample
	m.mu.Unlock()
	if cb != nil {
		cb(s)
	}
}
```

In `Poll`, add the emit call immediately after the existing `m.maybeAlert(w.name, smp)` line:

```go
		m.record(smp)
		m.maybeAlert(w.name, smp)
		m.emitSample(smp)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run TestMonitorOnSample -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add monitor_watch.go monitor_watch_test.go
git commit -m "feat(monitor): OnSample option — per-poll sample callback"
```

---

## Task 2: `UsageSink` + `JSONLSink`

**Files:**
- Create: `sink.go`
- Test: `sink_test.go`

**Interfaces:**
- Consumes: `Sample`, `Alert`, `LimitStatus`, `LimitWindow`.
- Produces:
  - `type UsageSink interface { WriteSample(Sample) error; WriteAlert(Alert) error; Close() error }`
  - `func NewJSONLSink(path string) (*JSONLSink, error)` — `*JSONLSink` implements `UsageSink`; writes one JSON line per **changed** sample and one per alert.

- [ ] **Step 1: Write the failing tests**

Create `sink_test.go`:

```go
package corral

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad json line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func sampleAt(provider string, used float64, ts time.Time) Sample {
	return Sample{
		Provider: provider,
		Worst:    used,
		Status:   &LimitStatus{Plan: "p", Source: "s", Windows: []LimitWindow{{Name: "5h", UsedPercent: used}}},
		At:       ts,
	}
}

func TestJSONLSink_DedupsUnchangedSamples(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	sink, err := NewJSONLSink(path)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Unix(1_700_000_000, 0).UTC()
	if err := sink.WriteSample(sampleAt("claude", 42, ts)); err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteSample(sampleAt("claude", 42, ts.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	_ = sink.Close()

	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("unchanged sample written %d times, want 1 (dedup)", len(lines))
	}
	if lines[0]["event"] != "sample" || lines[0]["provider"] != "claude" || lines[0]["worst_pct"].(float64) != 42 {
		t.Fatalf("unexpected record: %v", lines[0])
	}
}

func TestJSONLSink_WritesOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	sink, _ := NewJSONLSink(path)
	ts := time.Unix(1_700_000_000, 0).UTC()
	_ = sink.WriteSample(sampleAt("claude", 42, ts))
	_ = sink.WriteSample(sampleAt("claude", 55, ts)) // worst changed
	_ = sink.Close()
	if lines := readLines(t, path); len(lines) != 2 {
		t.Fatalf("change produced %d lines, want 2", len(lines))
	}
}

func TestJSONLSink_WritesAlert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	sink, _ := NewJSONLSink(path)
	_ = sink.WriteAlert(Alert{Provider: "claude", Worst: 81, Threshold: 80, At: time.Unix(1_700_000_000, 0).UTC()})
	_ = sink.Close()
	lines := readLines(t, path)
	if len(lines) != 1 || lines[0]["event"] != "alert" || lines[0]["threshold"].(float64) != 80 {
		t.Fatalf("alert record wrong: %v", lines)
	}
}

func TestJSONLSink_ErrSampleNoWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	sink, _ := NewJSONLSink(path)
	_ = sink.WriteSample(Sample{Provider: "grok", Err: "usage timeout", At: time.Unix(1_700_000_000, 0).UTC()})
	_ = sink.Close()
	lines := readLines(t, path)
	if len(lines) != 1 || lines[0]["err"] != "usage timeout" {
		t.Fatalf("err sample record wrong: %v", lines)
	}
	if _, hasWindows := lines[0]["windows"]; hasWindows {
		t.Fatalf("err sample should omit windows, got: %v", lines[0])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test . -run TestJSONLSink -v`
Expected: FAIL — `undefined: NewJSONLSink`.

- [ ] **Step 3: Implement the sink**

Create `sink.go`:

```go
package corral

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// UsageSink persists usage snapshots and alerts. It is the pluggable output seam
// for `corral serve`; JSONLSink is the default (append-only JSONL) implementation.
type UsageSink interface {
	WriteSample(Sample) error
	WriteAlert(Alert) error
	Close() error
}

// jsonlWindow / jsonlRecord are the on-disk JSONL shapes (snake_case).
type jsonlWindow struct {
	Name     string  `json:"name"`
	UsedPct  float64 `json:"used_pct"`
	ResetsAt string  `json:"resets_at,omitempty"`
}

type jsonlRecord struct {
	TS        string        `json:"ts"`
	Event     string        `json:"event"` // "sample" | "alert"
	Provider  string        `json:"provider"`
	Plan      string        `json:"plan,omitempty"`
	Source    string        `json:"source,omitempty"`
	WorstPct  float64       `json:"worst_pct"`
	Threshold float64       `json:"threshold,omitempty"`
	Windows   []jsonlWindow `json:"windows,omitempty"`
	Err       string        `json:"err,omitempty"`
}

// JSONLSink appends one JSON line per CHANGED sample and one per alert. It keeps
// a per-provider signature and skips samples whose signature is unchanged since
// the last written line, so an idle fleet produces almost no output. Safe for
// concurrent use (the serve signal handler's Close can race the poll goroutine).
type JSONLSink struct {
	mu      sync.Mutex
	f       *os.File
	enc     *json.Encoder
	lastSig map[string]string
}

// NewJSONLSink opens (creating parent dirs and the file) path for append.
func NewJSONLSink(path string) (*JSONLSink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir usage-log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open usage log: %w", err)
	}
	return &JSONLSink{f: f, enc: json.NewEncoder(f), lastSig: map[string]string{}}, nil
}

// sampleSignature is a stable string capturing everything a "change" cares about:
// worst percent (0.1 granularity), each window (name/used/reset), and the error.
func sampleSignature(s Sample) string {
	var b strings.Builder
	fmt.Fprintf(&b, "w=%.1f|", s.Worst)
	if s.Status != nil {
		for _, win := range s.Status.Windows {
			fmt.Fprintf(&b, "%s=%.1f@%d;", win.Name, win.UsedPercent, win.ResetsAt.Unix())
		}
	}
	b.WriteString("e=" + s.Err)
	return b.String()
}

func recordFromSample(s Sample) jsonlRecord {
	r := jsonlRecord{
		TS:       s.At.UTC().Format(time.RFC3339),
		Event:    "sample",
		Provider: s.Provider,
		WorstPct: s.Worst,
		Err:      s.Err,
	}
	if s.Status != nil {
		r.Plan = s.Status.Plan
		r.Source = s.Status.Source
		for _, win := range s.Status.Windows {
			jw := jsonlWindow{Name: win.Name, UsedPct: win.UsedPercent}
			if !win.ResetsAt.IsZero() {
				jw.ResetsAt = win.ResetsAt.UTC().Format(time.RFC3339)
			}
			r.Windows = append(r.Windows, jw)
		}
	}
	return r
}

// WriteSample writes a line only when this provider's signature changed since the
// last written line (the first sample per provider is always a baseline write).
func (s *JSONLSink) WriteSample(smp Sample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sig := sampleSignature(smp)
	if prev, ok := s.lastSig[smp.Provider]; ok && prev == sig {
		return nil
	}
	s.lastSig[smp.Provider] = sig
	return s.enc.Encode(recordFromSample(smp)) // json.Encoder appends '\n'
}

// WriteAlert writes an alert line (alerts are already debounced by the Monitor).
func (s *JSONLSink) WriteAlert(a Alert) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enc.Encode(jsonlRecord{
		TS:        a.At.UTC().Format(time.RFC3339),
		Event:     "alert",
		Provider:  a.Provider,
		WorstPct:  a.Worst,
		Threshold: a.Threshold,
	})
}

func (s *JSONLSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.f.Close()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test . -run TestJSONLSink -v && go vet ./...`
Expected: all PASS; vet clean.

- [ ] **Step 5: Commit**

```bash
git add sink.go sink_test.go
git commit -m "feat(sink): UsageSink + JSONLSink with per-provider change dedup"
```

---

## Task 3: `corral serve` command

**Files:**
- Create: `cmd/corral/serve.go`
- Test: `cmd/corral/serve_test.go`
- Modify: `cmd/corral/main.go`

**Interfaces:**
- Consumes: `corral.NewMonitor`, `corral.WithInterval`, `corral.WithThreshold`, `corral.OnSample` (Task 1), `corral.OnAlert`, `corral.Monitor.Watch`, `corral.Monitor.Poll`/`Run`, `corral.NewJSONLSink` + `corral.UsageSink` (Task 2), `corral.UsageReporter`, `corral.ProviderByName`, `canonicalProviders()`, `homePath()`.
- Produces: `func newServeCmd() *cobra.Command`; `func runServe(ctx, []serveWatch, corral.UsageSink, serveOpts) error` (testable core).

- [ ] **Step 1: Write the failing test**

Create `cmd/corral/serve_test.go`:

```go
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/inovacc/corral"
)

// fakeUsage is a UsageReporter with a fixed snapshot for serve tests.
type fakeUsage struct{ used float64 }

func (f fakeUsage) Usage() (*corral.LimitStatus, error) {
	return &corral.LimitStatus{Plan: "test", Windows: []corral.LimitWindow{{Name: "5h", UsedPercent: f.used}}}, nil
}

func TestRunServe_OnceWritesBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	sink, err := corral.NewJSONLSink(path)
	if err != nil {
		t.Fatal(err)
	}
	watches := []serveWatch{{name: "fake", reporter: fakeUsage{used: 42}}}
	if err := runServe(context.Background(), watches, sink, serveOpts{interval: time.Minute, threshold: 80, once: true}); err != nil {
		t.Fatalf("runServe: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(ln) != "" {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("serve --once wrote %d lines, want 1 baseline\n%s", lines, b)
	}
	if !strings.Contains(string(b), `"provider":"fake"`) || !strings.Contains(string(b), `"worst_pct":42`) {
		t.Fatalf("baseline line missing expected fields: %s", b)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/corral/ -run TestRunServe_OnceWritesBaseline -v`
Expected: FAIL — `undefined: serveWatch` / `undefined: runServe`.

- [ ] **Step 3: Implement the serve command**

Create `cmd/corral/serve.go`:

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/inovacc/corral"
	_ "github.com/inovacc/corral/all" // register providers
)

// serveWatch pairs a display name with its usage reporter.
type serveWatch struct {
	name     string
	reporter corral.UsageReporter
}

// serveOpts are the recorder loop knobs.
type serveOpts struct {
	interval  time.Duration
	threshold float64
	once      bool
}

// resolveServeWatches turns provider names into watchable (name, reporter) pairs,
// skipping unknown providers and those without a usage endpoint (returned as
// human-readable skip reasons for the caller to surface).
func resolveServeWatches(names []string) (watches []serveWatch, skipped []string) {
	for _, name := range names {
		p, err := corral.ProviderByName(name)
		if err != nil {
			skipped = append(skipped, name+" (unknown provider)")
			continue
		}
		ur, ok := p.(corral.UsageReporter)
		if !ok {
			skipped = append(skipped, p.Name()+" (no usage endpoint)")
			continue
		}
		watches = append(watches, serveWatch{name: p.Name(), reporter: ur})
	}
	return watches, skipped
}

// runServe is the testable recorder core: wire the Monitor to the sink and run
// (once, or until ctx is cancelled), then flush the sink.
func runServe(ctx context.Context, watches []serveWatch, sink corral.UsageSink, opts serveOpts) error {
	mon := corral.NewMonitor(
		corral.WithInterval(opts.interval),
		corral.WithThreshold(opts.threshold),
		corral.OnSample(func(s corral.Sample) {
			if err := sink.WriteSample(s); err != nil {
				slog.Error("serve: sink write sample", "provider", s.Provider, "err", err)
			}
		}),
		corral.OnAlert(func(a corral.Alert) {
			slog.Warn("usage threshold crossed", "provider", a.Provider, "worst_pct", a.Worst, "threshold", a.Threshold)
			if err := sink.WriteAlert(a); err != nil {
				slog.Error("serve: sink write alert", "provider", a.Provider, "err", err)
			}
		}),
	)
	for _, w := range watches {
		mon.Watch(w.name, w.reporter)
	}
	if opts.once {
		mon.Poll(ctx)
	} else {
		mon.Run(ctx) // blocks until ctx cancelled
	}
	return sink.Close()
}

// splitCSV splits a comma list, trimming spaces and dropping empties.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// newServeCmd builds `corral serve`.
func newServeCmd() *cobra.Command {
	var (
		out       string
		interval  time.Duration
		threshold float64
		providers string
		once      bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run a usage-recorder daemon: poll provider usage and append changes to a JSONL file",
		Long: "Continuously poll each subscription provider's usage and append a JSON line to\n" +
			"--out whenever a provider's usage CHANGES (plus a line per threshold alert).\n" +
			"An idle fleet produces almost no output. Ctrl-C flushes and exits.\n" +
			"Examples: `corral serve`, `corral serve --interval 2m --out ./usage.jsonl`, `corral serve --once`.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			names := canonicalProviders()
			if strings.TrimSpace(providers) != "" {
				names = splitCSV(providers)
			}
			watches, skipped := resolveServeWatches(names)
			for _, s := range skipped {
				cmd.PrintErrf("skipping %s\n", s)
			}
			if len(watches) == 0 {
				return fmt.Errorf("no providers with a usage endpoint to watch (resolved: %v)", names)
			}

			sink, err := corral.NewJSONLSink(out)
			if err != nil {
				return fmt.Errorf("open usage log %q: %w", out, err)
			}

			ctx := context.Background()
			if !once {
				var stop context.CancelFunc
				ctx, stop = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
				defer stop()
				cmd.PrintErrf("corral serve: recording %d provider(s) to %s every %s (Ctrl-C to stop)\n",
					len(watches), out, interval)
			}
			return runServe(ctx, watches, sink, serveOpts{interval: interval, threshold: threshold, once: once})
		},
	}
	cmd.Flags().StringVar(&out, "out", homePath(".corral", "usage.jsonl"), "JSONL output path")
	cmd.Flags().DurationVar(&interval, "interval", 5*time.Minute, "poll cadence")
	cmd.Flags().Float64Var(&threshold, "threshold", 80, "alert threshold percent (0..100; <=0 disables alerting)")
	cmd.Flags().StringVar(&providers, "providers", "", "comma-separated providers to watch (default: all with a usage endpoint)")
	cmd.Flags().BoolVar(&once, "once", false, "poll once, write, and exit")
	return cmd
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/corral/ -run TestRunServe_OnceWritesBaseline -v`
Expected: PASS.

- [ ] **Step 5: Wire `serve` into `main.go`**

In `cmd/corral/main.go`:

Add `"serve"` to `isLightCommand`:

```go
func isLightCommand(arg string) bool {
	switch arg {
	case "usage", "launch", "serve":
		return true
	}
	return false
}
```

Register in the light root (the `isLightCommand` branch):

```go
		light.AddCommand(newUsageCmd(), newLaunchCmd(), newServeCmd())
```

And in the full root:

```go
	root.AddCommand(newUsageCmd(), newLaunchCmd(), newServeCmd())
```

- [ ] **Step 6: Verify build + command registration + full suite**

Run: `go build ./... && go run ./cmd/corral serve --help && go test ./...`
Expected: build clean; `--help` shows `serve` with `--out --interval --threshold --providers --once`; all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/corral/serve.go cmd/corral/serve_test.go cmd/corral/main.go
git commit -m "feat(cmd): corral serve — usage-recorder daemon (JSONL change log)"
```

---

## Self-Review Notes

- **Spec coverage:** §2.1 UsageSink/JSONLSink → Task 2; §2.2 Monitor.OnSample → Task 1; §2.3 serve command + main.go wiring → Task 3; §3 dedup semantics + JSONL schema → Task 2 (`sampleSignature` + `jsonlRecord`); §4 flags → Task 3; §5 error handling (non-fatal sink writes via slog, fatal startup open, signal shutdown, `--once`) → Task 3 `runServe`/`newServeCmd`; §6 tests → each task's tests.
- **Type consistency:** `serveWatch{name, reporter}` and `serveOpts{interval, threshold, once}` and `runServe(ctx, []serveWatch, corral.UsageSink, serveOpts)` are defined in Task 3 and used by its own test. `UsageSink`/`NewJSONLSink` defined in Task 2, consumed in Task 3. `OnSample` defined in Task 1, consumed in Task 3.
- **Deferred (spec §7 non-goals, intentionally not built):** HTTP endpoint, log rotation, a second sink impl.
- **Cross-platform note:** `syscall.SIGTERM` is a valid `os.Signal` on Windows (never delivered there, harmless); `os.Interrupt` is the effective stop signal. `signal.NotifyContext` with both is correct on all targets.
