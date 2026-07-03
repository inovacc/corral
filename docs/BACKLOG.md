# Backlog

Prioritized future work and tech debt for `github.com/inovacc/corral`.
Effort key: **S** (< 1 day) · **M** (1–3 days) · **L** (> 3 days).

## P2 — High value / next up

- **[M] `usage.go` + `UsageReporter` for grok, kimi, qwen.** Only `agy/`, `claude/`, and `codex/` ship a `UsageReporter` today; `grok/`, `kimi/`, and `qwen/` have none. Each provider's quota/usage endpoint needs reverse-engineering (grok x.ai, Moonshot Kimi, Alibaba Qwen) before a `LimitStatus` can be surfaced through `Monitor`.
- **[M] SessionPool backoff + attempt cap (`session.go`).** The reopen path can hot-loop when a warm session keeps failing to come back. Add exponential backoff plus a bounded attempt cap so a dead provider degrades instead of spinning.
- **[S] `checkLimit` backpressure under concurrency (`monitor.go`).** Concurrent callers can stampede the limit check; add backpressure so quota probes are coalesced/serialized rather than fired per-goroutine.
- **[M] Pluggable `UsageSink` to persist `LimitStatus`.** Introduce a sink seam so `Monitor` can hand `LimitStatus` snapshots to a caller-supplied persister (DB, metrics, file) instead of holding them only in memory.
- **[L] Raise test coverage 61.2% → 80%.** Focus on the under-covered provider packages and the session/monitor state machines.

## P3 — Later / opportunistic

- **[L] pixkb consumer refactor.** Repoint `pixkb` at `github.com/inovacc/corral` and delete its vendored `pkg/agents` copy, closing the extraction loop.
- **[L] Additional provider backends.** Grow the shipped roster beyond the current six (claude, codex, agy, grok, kimi, qwen) as new subscription coding-agent CLIs stabilize.
- **[S] Verify grok/kimi stdin support for oversized prompts.** `PromptFlag` providers route oversized prompts to stdin, but grok's and kimi's stdin handling is unverified — confirm end-to-end or add a per-provider fallback.
