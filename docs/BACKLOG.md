# Backlog

Prioritized future work and tech debt for `github.com/inovacc/corral`.
Effort key: **S** (< 1 day) · **M** (1–3 days) · **L** (> 3 days).

## P2 — High value / next up

- **[M] Component execution-mode selection (API vs subscription).** At component creation, let the user pick each vendor's execution backend: **subscription-based** (corral's default — headless subscription CLI; the creation flow needs/validates access to the agent config folder, e.g. `~/.claude`, `~/.gemini`, `~/.codex`, `~/.antigravity`) OR **api-based** (metered API — the flow collects API info: key/endpoint/model, keys referenced via env vars, never hardcoded). Add `execution: {mode: subscription|api, config: {...}}` per vendor to the component spec (`corral.json`); the generated Go-module wrapper's `New()` wires the chosen provider (`CLIProvider` for subscription, an API provider for api). Reconciles the subscription-vs-metered-API tension (surfaced by the adk-go eval) *inside* the component generator — one spec, two execution backends, chosen at creation. Builds on the component-generator milestone (`feat/agent-control-plane`). (2026-07-04.)
- ~~**[M] `usage.go` + `UsageReporter` for grok, kimi.**~~ **DONE (verified 2026-07-04)** — both shipped: `grok/usage.go` `FetchUsage(ctx)` (real `cli-chat-proxy.grok.com/v1/billing` call, `~/.grok/auth.json`/`XAI_API_KEY` auth, KB-derived field mapping, graceful non-blocking) wired via `grok.Provider.Usage()`; `kimi/usage.go` `ReadUsage(ctx)`/`decodeUsage` (real `/usages` endpoint, `~/.kimi-code/credentials` OAuth store) wired via `kimi.Provider.Usage()`. Both have `usage_test.go` (network-free decode/map seams). All five providers (agy/claude/codex/grok/kimi) now implement `UsageReporter`.
- **[M] SessionPool backoff + attempt cap (`session.go`).** The reopen path can hot-loop when a warm session keeps failing to come back. Add exponential backoff plus a bounded attempt cap so a dead provider degrades instead of spinning.
- **[S] `checkLimit` backpressure under concurrency (`monitor.go`).** Concurrent callers can stampede the limit check; add backpressure so quota probes are coalesced/serialized rather than fired per-goroutine.
- **[S] Context-aware `UsageReporter.Usage(ctx)`.** `Monitor.Poll` only checks `ctx` *between* providers; a provider's blocking `Usage()` HTTP call ignores cancellation, so SIGINT during a hung poll (and `corral serve --once` against a hung provider) can't be promptly bounded. Thread a `context.Context` into `Usage` so shutdown/timeout is honored mid-call. (Surfaced by the `corral serve` whole-feature review, 2026-07-04.)
  - ~~Pluggable `UsageSink` to persist `LimitStatus`~~ — **DONE 2026-07-04:** `UsageSink` + `JSONLSink` + `Monitor.OnSample` seam shipped via `corral serve` (append-only JSONL change log).
- **[L] Raise test coverage 61.2% → 80%.** Focus on the under-covered provider packages and the session/monitor state machines.

## P3 — Later / opportunistic

- **[L] pixkb consumer refactor.** Repoint `pixkb` at `github.com/inovacc/corral` and delete its vendored `pkg/agents` copy, closing the extraction loop.
- **[L] Additional provider backends.** Grow the shipped roster beyond the current five (claude, codex, agy, grok, kimi) as new subscription coding-agent CLIs stabilize. **(Not actionable as a discrete task — this is a standing placeholder, not buildable until a specific new CLI is chosen. When one is: implement `<name>/` with `Provider`+`CLIProvider` config, a `usage.go`/`UsageReporter` if it exposes a quota endpoint, register in the barrel, and add `<name>_test.go`. Reassessed 2026-07-04.)**
- **[S] Verify grok/kimi stdin support for oversized prompts.** `PromptFlag` providers route oversized prompts to stdin, but grok's and kimi's stdin handling is unverified — confirm end-to-end or add a per-provider fallback.
