# corral `api` Execution Mode + API Providers — Design

**Date:** 2026-07-05
**Status:** Approved (brainstorm) — pending spec review → implementation plan
**Backlog item:** `#1` Component execution-mode selection (API vs subscription)

## Goal

Let a corral component choose, **per vendor at creation time**, whether that
vendor executes agent turns via its **subscription CLI** (today's only path) or
via a **metered API**. Deliver the `api` path for real by adding API-based
`Provider` backends — with **OpenRouter (via the official Go SDK)** as the
flagship universal backend, plus direct **OpenAI-compatible** and **Anthropic
Messages** backends — and wiring the choice through the component generator.

## Context (current state)

- Every agent turn runs through `CLIProvider` (subscription coding-agent CLIs:
  claude/codex/grok/kimi + agy's ConPTY driver). There is **no API turn
  provider** — `codex/api.go` is only codex's usage-reporting HTTP path, and
  `embed.go`'s OpenAI use is embeddings, not turns.
- The `Provider` seam is `Name() string` + `Run(ctx, RunRequest) (RunResult,
  error)`; `UsageReporter` (`Usage(ctx) (*LimitStatus, error)`) is optional and
  now ctx-aware (item #5). The Agency gates turns on it and coalesces/backs-off
  (items #4/#3). Any new `Provider` drops into all of this unchanged.
- The component generator (`aihost/`) renders `corral.json` → per-vendor plugin
  trees + a Go-module wrapper whose `New(provider)` calls `NewAgency(provider,
  ".")`.

## Dependency-thesis amendment (recorded decision)

corral's founding thesis was **one external dependency (`conpty`)**. This
feature deliberately amends it to **two**: `conpty` +
`github.com/OpenRouterTeam/go-sdk` (Apache-2.0, beta — pinned `@v0.5.9`,
requires Go 1.25+; corral is on Go 1.26.x). Rationale: OpenRouter is the
universal `api` backend (400+ models via `vendor/model` slugs incl. routing,
guardrails, analytics), and the user chose the typed SDK over a hand-rolled
OpenAI-compatible client for it. The two hand-rolled backends (`openai`,
`anthropic`) add **no** further deps. This amendment will be reflected in
`CLAUDE.md` and `docs/BACKLOG.md`.

## Architecture

### 1. `api` backends (each a `Provider`)

`api` mode selects a backend by a config `format`. Each is a standalone
`Provider` (and optional `UsageReporter`), so the Agency/pool/monitor need no
changes.

| `format` | Backend | Covers | Dep |
|---|---|---|---|
| `openrouter` *(default)* | `OpenRouterTeam/go-sdk` (`openrouter.New(WithSecurity(key))`, `s.Chat.Send`) | any model via `vendor/model` slug (claude, gpt, grok, gemini, kimi) + routing | go-sdk |
| `openai` | hand-rolled OpenAI Chat Completions (`POST {base}/chat/completions`, Bearer) | direct OpenAI / xAI / Moonshot / gemini-compat | none |
| `anthropic` | hand-rolled Anthropic Messages (`POST {base}/v1/messages`, `x-api-key`) | direct claude | none |

Because OpenRouter routes to every model, `openrouter` alone can back **all five
vendors**; the direct formats are for hitting a vendor API without OpenRouter in
the path.

### 2. `Run` semantics (all backends)

`Run(ctx, req)`:
1. `prompt := req.ComposePrompt(embedSchema)` — reuse the existing composer;
   `embedSchema = true` unless the backend has native structured output for the
   request's schema.
2. Build a single-turn chat request: one `user` message = `prompt` (+ optional
   `system` from the Agent), `model` from config, `ctx` threaded.
3. POST; map the first choice's message content → `RunResult{Text, Provider}`.
4. On a typed API error, return `fmt.Errorf("%s: %w", name, err)`; on ctx
   cancellation, return promptly (ctx is threaded into every call).

### 3. Structured output (`Agent.Schema`)

- `openrouter`/`openai` → `response_format: {type: json_schema, ...}` when the
  model supports it; the SDK/endpoint returns JSON, parsed into `RunResult.Text`.
- `anthropic` → tool-use (single forced tool whose input schema = the request
  schema).
- Any backend lacking native support → prompt-embedded schema + parse the JSON
  out of the reply (the exact fallback `CLIProvider` already uses).

### 4. `UsageReporter`

Map each backend's rate-limit signal → `LimitStatus` so `api` mode participates
in the item-#4/#3 coalesce+backoff machinery:
- OpenAI/OpenRouter: `x-ratelimit-remaining-*` / OpenRouter usage accounting.
- Anthropic: `anthropic-ratelimit-*` headers.
Absent/unparseable → `(nil, nil)` (non-blocking, matching the CLI providers).

### 5. Config schema (`corral.json`)

Per vendor, additive and backward-compatible (absent `execution` ⇒
`subscription`, today's behavior):

```json
{
  "vendors": {
    "claude": {
      "execution": {
        "mode": "api",
        "config": {
          "format": "openrouter",
          "model": "anthropic/claude-sonnet-4",
          "key_env": "OPENROUTER_API_KEY",
          "base_url": "",
          "routing": { "http_referer": "https://…", "x_title": "corral", "provider": {} }
        }
      }
    },
    "codex": { "execution": { "mode": "subscription" } }
  }
}
```

Rules:
- **`key_env` is an env-var NAME**, never a literal key. Validation errors if a
  literal-looking secret is placed there.
- `subscription` config validates access to the vendor's config folder
  (`~/.claude`, `~/.codex`, `~/.gemini`, `~/.antigravity`) — surfaced as a
  Doctor/validation warning, not a hard failure at generate time.
- `base_url` optional (defaults per format); `routing` optional (OpenRouter).

### 6. Generator wiring (`aihost/`)

- `aihost/ir.go`: extend the per-vendor IR with an `Execution` struct
  (`Mode`, `Config{Format, Model, BaseURL, KeyEnv, Routing}`); `Load` parses it
  (absent ⇒ `subscription`), validates `mode`/`format` enums and the
  `key_env`-not-a-literal rule.
- Generated `New(provider string)`: branch on the vendor's execution mode —
  `subscription` → the CLIProvider preset (unchanged); `api` → construct the
  chosen API backend from config (`key := os.Getenv(cfg.KeyEnv)`).
- Keep `New(provider)`'s existing decoupling (item from the prior generator
  fix): the runtime provider name is still the caller's, and generation never
  bakes a key in.

### 7. Testing (no live keys, no network)

- Hand-rolled backends: `httptest.Server` returning canned chat/messages
  bodies; assert the **outgoing request** (model, messages, auth header, schema
  wiring) and the **response mapping** (→ `RunResult`), plus a ctx-cancel case
  and a rate-limit-header → `LimitStatus` case.
- OpenRouter backend: point the SDK at the stub via `WithServer(ts.URL)` (or a
  custom `http.Client`); assert a turn maps correctly and a typed
  `sdkerrors.*` surfaces as a wrapped error.
- Generator: golden `corral.json` with mixed subscription/api vendors → assert
  the generated `New()` branches and the IR parse/validation (bad `mode`,
  literal-key-in-`key_env`, unknown `format`).

## File structure

- **New** `apiprovider/` (or top-level `apiprovider.go`): the `openai` +
  `anthropic` hand-rolled backends behind a small shared request/response shape;
  each a `Provider` (+ `UsageReporter`).
- **New** `openrouter/` sub-package: the go-sdk-backed backend (isolates the dep
  to one package; the core stays importable without pulling routing types into
  every consumer).
- **Modify** `aihost/ir.go` + `aihost/gen.go` (or the `component.gen.go`
  template): `Execution` IR, `Load` parse/validate, generated `New()` branch.
- **Modify** `go.mod`: add `github.com/OpenRouterTeam/go-sdk v0.5.9`.
- **New** tests alongside each (`*_test.go`), all network-free.
- **Modify** `CLAUDE.md` + `docs/BACKLOG.md`: record the two-dep thesis; mark #1
  progress.

## Non-goals / deferred

- Streaming responses (single-turn non-streaming only for now).
- Embeddings/rerank/TTS/video/Responses API from the SDK (turns only).
- Retrofitting existing subscription providers — they stay CLI-based; `api` is
  purely additive.
- A UI/interactive `corral.json` builder — config is authored directly for now.

## Open items to resolve during planning (not blockers)

- The go-sdk `Chat.Send` exact request/response types + how `response_format`
  and custom `http.Client` are passed (pull `docs/sdks/chat` when planning).
- Whether the three backends share enough to warrant a common
  `apiTurn(ctx, httpReq)` helper or stay separate (decide from the real shapes).
