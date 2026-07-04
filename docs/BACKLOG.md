# Backlog

Prioritized future work and tech debt for `github.com/inovacc/corral`.
Effort key: **S** (< 1 day) · **M** (1–3 days) · **L** (> 3 days).

## P2 — High value / next up

- **[M] `usage.go` + `UsageReporter` for grok, kimi.** Only `agy/`, `claude/`, and `codex/` ship a `UsageReporter` today; `grok/` and `kimi/` have none. Each provider's quota/usage endpoint needs reverse-engineering (grok x.ai, Moonshot Kimi) before a `LimitStatus` can be surfaced through `Monitor`.
- **[M] SessionPool backoff + attempt cap (`session.go`).** The reopen path can hot-loop when a warm session keeps failing to come back. Add exponential backoff plus a bounded attempt cap so a dead provider degrades instead of spinning.
- **[S] `checkLimit` backpressure under concurrency (`monitor.go`).** Concurrent callers can stampede the limit check; add backpressure so quota probes are coalesced/serialized rather than fired per-goroutine.
- **[S] Context-aware `UsageReporter.Usage(ctx)`.** `Monitor.Poll` only checks `ctx` *between* providers; a provider's blocking `Usage()` HTTP call ignores cancellation, so SIGINT during a hung poll (and `corral serve --once` against a hung provider) can't be promptly bounded. Thread a `context.Context` into `Usage` so shutdown/timeout is honored mid-call. (Surfaced by the `corral serve` whole-feature review, 2026-07-04.)
  - ~~Pluggable `UsageSink` to persist `LimitStatus`~~ — **DONE 2026-07-04:** `UsageSink` + `JSONLSink` + `Monitor.OnSample` seam shipped via `corral serve` (append-only JSONL change log).
- **[L] Raise test coverage 61.2% → 80%.** Focus on the under-covered provider packages and the session/monitor state machines.

## P3 — Later / opportunistic

- **[L] pixkb consumer refactor.** Repoint `pixkb` at `github.com/inovacc/corral` and delete its vendored `pkg/agents` copy, closing the extraction loop.
- **[L] Additional provider backends.** Grow the shipped roster beyond the current five (claude, codex, agy, grok, kimi) as new subscription coding-agent CLIs stabilize.
- **[S] Verify grok/kimi stdin support for oversized prompts.** `PromptFlag` providers route oversized prompts to stdin, but grok's and kimi's stdin handling is unverified — confirm end-to-end or add a per-provider fallback.
