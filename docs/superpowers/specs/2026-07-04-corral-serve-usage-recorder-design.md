# `corral serve` — Usage Recorder Daemon — Design

**Status:** Approved to plan · 2026-07-04 · module `github.com/inovacc/corral`

## 1. Problem

corral can query subscription-agent usage on demand (`corral usage`), and it has a continuous `Monitor` (`monitor_watch.go`) that polls providers, keeps a per-provider `Sample` trail, and fires threshold `Alert`s. What's missing is a **long-running service** that runs that monitor unattended and **persists usage changes to disk** for later analysis — the queued "pluggable `UsageSink` → JSONL file sink" backlog item. `corral serve` is that daemon.

**Scope decision (confirmed):** `serve` is a **recorder daemon, not an HTTP server**. It runs continuously and appends usage *changes* to a JSONL file. No network endpoint (YAGNI — the deliverable is the file). An HTTP `/usage` endpoint is explicitly out of scope; it can be a later, separate feature.

## 2. Architecture

Reuse the existing `Monitor` wholesale. Add one small generic seam to it, and a pluggable sink. Three units:

### 2.1 `UsageSink` interface + `JSONLSink` — `sink.go` (package `corral`)
```go
type UsageSink interface {
	WriteSample(Sample) error
	WriteAlert(Alert) error
	Close() error
}
```
- `JSONLSink` is the one implementation: appends one JSON line per **changed** sample and one per alert to an `io.Writer`/file it owns. Constructed with `NewJSONLSink(path string) (*JSONLSink, error)` (opens the file `O_CREATE|O_WRONLY|O_APPEND`, creating parent dirs). Thread-safe via an internal mutex (the signal-handler `Close` can race the poll goroutine).
- The interface exists so a future `stdout`/webhook sink drops in without touching `serve` — directly satisfies the backlog's "pluggable" framing. YAGNI holds it to one interface + one impl now.

### 2.2 `Monitor` gains `OnSample(func(Sample))` — `monitor_watch.go`
A new option mirroring the existing `OnAlert`, plus an `onSample func(Sample)` field. `Monitor.Poll` calls it once per provider after `record()` (and after `maybeAlert`), so the host sees every poll. Small, generic, reusable — the Monitor stays sink-agnostic; `serve` owns the dedup + write.

### 2.3 `newServeCmd()` — `cmd/corral/serve.go`
`corral serve [flags]`:
1. Resolve the provider set (canonical providers with a `UsageReporter`, via the existing `canonicalProviders()`/`ProviderByName` + `Monitor.WatchProvider`, which skips providers with no queryable limit).
2. Open the `JSONLSink` at `--out`.
3. Build `NewMonitor(WithInterval, WithThreshold, OnSample(→ sink.WriteSample with change-dedup), OnAlert(→ sink.WriteAlert + slog.Warn))`.
4. Run `Monitor.Run(ctx)` under `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`; on signal, cancel → `Run` returns → `sink.Close()` (flush).
5. Registered in `cmd/corral/main.go`'s `isLightCommand` (like `usage`/`launch`) so it bypasses the mantle bootstrap (clean stderr logging, no stray `config.yaml`), and added to both the light root and the full root command sets.

## 3. Data flow & "store changes" semantics

Providers → `Monitor.Poll` (every `--interval`) → `Sample` → `OnSample` → `JSONLSink`. The sink keeps a **per-provider signature** = `worst_pct` (rounded to 0.1) + each window's `{name, used_pct(0.1), resets_at}` + `err`. It writes a line **only when the new signature differs from the last written** for that provider; the **first** sample per provider is always written (baseline). Result: an idle fleet produces almost no lines; a real shift produces exactly one line. Threshold crossings additionally emit an `alert` line via `OnAlert` (the Monitor already debounces alerts until recovery).

**JSONL schema** (one object per line, `snake_case`):
```json
{"ts":"2026-07-04T12:00:00Z","event":"sample","provider":"claude","plan":"Max","source":"oauth/usage","worst_pct":42.5,"windows":[{"name":"weekly-Fable","used_pct":42.5,"resets_at":"2026-07-07T00:00:00Z"}]}
{"ts":"2026-07-04T13:00:00Z","event":"alert","provider":"claude","worst_pct":81.0,"threshold":80.0}
{"ts":"2026-07-04T12:00:00Z","event":"sample","provider":"grok","err":"usage timeout"}
```
`event` is `"sample"` or `"alert"`. `windows`/`plan`/`source` are omitted (empty) when `Status` is nil (the `err` case). `ts` is the sample's `At` in RFC3339 UTC.

## 4. Flags & defaults
| Flag | Default | Meaning |
|------|---------|---------|
| `--out` | `~/.corral/usage.jsonl` | JSONL output path (parent dirs auto-created) |
| `--interval` | `5m` | poll cadence — a daemon must not hammer subscription usage endpoints every 60s |
| `--threshold` | `80` | alert threshold (percent, 0..100); `<=0` disables alerting |
| `--providers` | *(all canonical with usage)* | comma-separated subset to watch |
| `--once` | `false` | poll once, write, and exit (cron / testing) |

## 5. Error handling
- **Sink write error** → `slog.Error` to stderr, **keep running** (a transient file error must never kill the daemon).
- **Provider poll error** → already captured as `Sample.Err`; written as a change if the error state flips (so an outage is recorded once, and recovery once).
- **Startup `--out` open failure** → fatal (the daemon has no job without its sink).
- **SIGINT/SIGTERM** → cancel the `Monitor` context, `Run` returns, `sink.Close()` flushes and closes. `--once` takes the same path after a single `Poll`.
- All logging is `slog` to **stderr** (stdout stays clean; `serve` bypasses the mantle bootstrap so nothing pollutes it).

## 6. Testing (all network-free)
- **`JSONLSink`** (table tests to a `t.TempDir()` file, read back + assert line count/content): (a) same sample twice → **one** line (dedup); (b) changed `worst_pct`/window → a **new** line; (c) alert → an `event:"alert"` line; (d) `err` sample → a line with `err` and no `windows`; (e) first-sample-always-written baseline.
- **`Monitor.OnSample`**: unit test — `Poll` with a fake `UsageReporter` invokes the registered `OnSample` once per provider (and still records + alerts as before).
- **`corral serve --once`**: smoke test against a fake in-process provider registry (or an injected sink), asserting exactly one baseline line lands and the process exits 0.

## 7. Non-goals
- No HTTP/gRPC endpoint (recorder only).
- No log rotation / retention policy (append-only; rotation is the operator's / a later feature's job).
- No new provider or usage-endpoint work — `serve` only drives the existing `Monitor` + providers.
- No second sink implementation now (the interface is the extension point; build stdout/webhook sinks when actually needed).
