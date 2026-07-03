# Roadmap
<!-- rev:001 -->

Corral is a provider-abstracted runtime that drives subscription coding-agent
CLIs behind one `Provider` interface, with a warm `SessionPool`, rate-limit
awareness, a pluggable provider registry, and a per-host plugin installer.

## Current Status

**Overall Progress:** foundation complete — 6 provider backends live, single
external dependency, public repo. Focus now shifts to usage reporting parity,
session resilience, and lifting coverage toward 80%.

## Phase 1: Foundation [DONE]

- [x] Extract agent runtime from `pixkb/pkg/agents` into a standalone module (`18d639f`)
- [x] Brand the module as **Corral** (`agents` → `corral`) (`1f88a6e`)
- [x] Publish as a public repo at `github.com/inovacc/corral` (`1f88a6e`)
- [x] Real README + `BRANDING.md` name candidates (`6c18a90`)

## Phase 2: Provider Backends [DONE]

Six providers, each self-registering via `init()` and blank-imported by `all/`:

- [x] `claude/` — Anthropic Claude Code, headless `claude -p` (+ `UsageReporter`) (`18d639f`)
- [x] `codex/` — OpenAI Codex, headless `codex exec --output-schema` (+ `UsageReporter`, upstream drift pinning) (`18d639f`)
- [x] `agy/` — Google Antigravity, ConPTY pseudo-console + warm `Session`, Windows-only (+ `UsageReporter`) (`18d639f`)
- [x] `grok/` — x.ai Grok, headless `grok --single <prompt>` (aliases `xai`) (`7e28db7`)
- [x] `kimi/` — Moonshot Kimi Code, headless `kimi --prompt <prompt> --yolo` (aliases `kimi-code`, `moonshot`) (`7e28db7`)
- [x] `qwen/` — Alibaba Qwen Code, headless `qwen --prompt <prompt> --yolo`, gemini-cli fork (aliases `qwen-code`) (`7e28db7`)
- [x] Generalize `CLIProvider` with `PromptFlag` (+ oversized-prompt stdin fallback) (`7e28db7`)

## Phase 3: Usage & Resilience [NEXT]

- [ ] `UsageReporter` + `usage.go` for `grok/`, `kimi/`, and `qwen/` (currently none)
- [ ] `SessionPool` exponential backoff + attempt cap
- [ ] `checkLimit` concurrency backpressure
- [ ] Pluggable `UsageSink` for persisting `LimitStatus`

## Phase 4: Adoption & Hardening [NEXT]

- [ ] `pixkb` consumer refactor — repoint `pixkb` at this module
- [ ] Raise test coverage 61.2% → 80%

## Test Coverage

**Total:** 61.2% (`go tool cover`) — **Target:** 80%.

Tracked as a Phase 4 goal; verify with `task test` / `go test ./...` and read
the profile via `go tool cover`.
