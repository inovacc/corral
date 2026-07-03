# Features

<!-- rev:002 -->

`corral` is a provider-abstracted runtime that drives subscription coding-agent
CLIs behind one `Provider` interface. This document tracks shipped capabilities
and proposed work.

## Completed

- **Provider abstraction** — one `Provider` interface (`provider.go`) with five
  self-registering backends, each blank-imported through `all/`:
  - `claude/` — Anthropic Claude Code, headless `claude -p`.
  - `codex/` — OpenAI Codex, headless `codex exec --output-schema` with
    upstream drift pinning.
  - `agy/` — Google Antigravity, ConPTY pseudo-console driver (no headless
    mode), Windows-only.
  - `grok/` — x.ai Grok, headless `grok --single <prompt>` (aliases `xai`).
  - `kimi/` — Moonshot Kimi Code, headless `kimi --prompt <prompt> --yolo`
    (aliases `kimi-code`, `moonshot`).
- **Warm SessionPool** (`session.go`) — reuses live agent sessions to avoid
  cold-start cost per turn.
- **Caller-supplied roster** (`registry.go`) — `Register`/`All`/`ByName`; the
  library ships the machinery, the application chooses which agents to enlist.
- **Pluggable provider registry** (`providers.go`) — `RegisterProvider` /
  `ProviderByName` resolves backends by name and alias.
- **CLIProvider headless engine** (`cli.go`) — declarative flag mapping
  (`BaseArgs`, `ModelFlag`, `SchemaFlag`, `OutputFlag`, `DirFlag`, `PromptFlag`)
  with an oversized-prompt stdin fallback.
- **agy ConPTY driver** — pseudo-console session for the no-headless-mode
  Antigravity CLI, kept warm across turns.
- **Usage / rate-limit awareness** (`monitor.go`) — `UsageReporter` capability
  plus `LimitStatus` (`Worst`/`Exhausted`/`OK`) implemented for `claude`,
  `codex`, and `agy`.
- **Multi-host plugin installer** (`host/`) — installs the agent plugin per host.
- **Optional embeddings** (`embed.go`, `embedder.go`) — `Embedder` interface
  with `OpenAIEmbedder`; offline deterministic hashing is the default.
- **Doctor health checks** (`doctor.go`) — validates provider CLIs and environment.
- **Thin CLI** (`cmd/corral`) — a mantle-based command surface over the runtime.

## Proposed

- `UsageReporter` + `usage.go` for `grok` and `kimi`.
- Rate-limit backpressure — concurrency gating in `checkLimit` when a provider
  nears exhaustion.
- Pluggable `UsageSink` for persisting `LimitStatus` snapshots.
- Additional provider backends as new subscription coding-agent CLIs land.
