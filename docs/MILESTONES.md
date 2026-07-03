# Milestones

<!-- rev:001 -->

Version milestones for **Corral** — a provider-abstracted runtime that drives
subscription coding-agent CLIs behind one `Provider` interface, with a warm
`SessionPool`, rate-limit awareness, a pluggable provider registry, and a
per-host plugin installer.

Each milestone maps to the phases tracked in [`ROADMAP.md`](ROADMAP.md).

## v0.1.0 — Foundation (current, unreleased)

Runtime extracted from `pixkb/pkg/agents`, branded as **Corral**, and published
as a public module.

- Agent runtime extracted from `pixkb/pkg/agents` into a standalone module.
- Branded `agents` → `corral`; published at `github.com/inovacc/corral`
  (public, BSD-3-Clause).
- Six provider backends, each self-registering via `init()` and blank-imported
  through `all/`:
  - `claude/` — Anthropic Claude Code, headless `claude -p` (+ `UsageReporter`).
  - `codex/` — OpenAI Codex, headless `codex exec --output-schema`
    (+ `UsageReporter`, upstream drift pinning).
  - `agy/` — Google Antigravity, ConPTY pseudo-console + warm `Session`,
    Windows-only (+ `UsageReporter`).
  - `grok/` — x.ai Grok, headless `grok --single <prompt>` (aliases `xai`).
  - `kimi/` — Moonshot Kimi Code, headless `kimi --prompt <prompt> --yolo`
    (aliases `kimi-code`, `moonshot`).
  - `qwen/` — Alibaba Qwen Code, headless `qwen --prompt <prompt> --yolo`,
    gemini-cli fork (aliases `qwen-code`).
- Generalized `CLIProvider` with `PromptFlag` + oversized-prompt stdin fallback.
- Single external dependency: `github.com/UserExistsError/conpty`.
- Test coverage: **61.2%**.

## v0.2.0 — Usage & Resilience (planned)

Usage-reporting parity across all providers, session resilience, and a coverage
lift toward the 80% target.

- `UsageReporter` + `usage.go` for `grok/`, `kimi/`, and `qwen/` (currently
  none).
- `SessionPool` exponential backoff + attempt cap.
- `checkLimit` concurrency backpressure.
- Pluggable `UsageSink` for persisting `LimitStatus`.
- Test coverage target: **80%**.
