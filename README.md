# agents

> A provider-abstracted runtime for driving **subscription coding-agent CLIs**
> (Anthropic Claude Code, OpenAI Codex, Google Antigravity) behind one
> interface — with a warm session pool, rate-limit awareness, and a per-host
> plugin installer. Built on [mantle](https://github.com/inovacc/mantle).

`agents` runs a roster of **declarative agents** through a pluggable **Provider**
backend. Each backend is a real subscription coding agent in its own vendor
package, so the products stay cleanly separated. The roster is **caller-supplied**
— you register the agents your application needs; this module ships the machinery,
not a fixed set of agents.

Extracted from the [`pixkb`](https://github.com/inovacc/pixkb) agent host into a
standalone, reusable module.

## Providers (one product each)

| Package  | Product                | Mechanism |
|----------|------------------------|-----------|
| `claude/`| **Anthropic Claude Code** | headless `claude -p` |
| `codex/` | **OpenAI Codex**       | headless `codex exec` (`--output-schema`) + rate-limit usage tracking + upstream drift pinning |
| `agy/`   | **Google Antigravity** | ConPTY pseudo-console (no headless mode) + warm Session (Windows) |
| `all/`   | —                      | blank-imports the three so they self-register |
| `host/`  | —                      | multi-host plugin installer (Claude / Codex / Antigravity) |

## Core types

- **`Provider`** (`provider.go`) — `Name()` + `Run(ctx, RunRequest) (RunResult, error)`; optional warm sessions via a `SessionOpener` type-assertion.
- **`Agent`** (`agent.go`) — declarative: Name, Kind, Description, Model, Tools, System prompt, Schema.
- **Provider registry** (`providers.go`) — `RegisterProvider(factory, names…)` (called from each vendor `init()`); `ProviderByName(name)`.
- **Agent roster** (`registry.go`) — `Register` / `All` / `ByName`; caller-supplied.
- **`SessionPool`** (`session.go`) — one warm session per agent, reused across turns, reopened on death.
- **`Agency`** (`agency.go`) — pairs a Provider with the roster; warm when the provider supports it.
- **`CLIProvider`** (`cli.go`) — the shared headless-CLI mechanism the codex/claude presets build on.
- **`Doctor`** (`doctor.go`) — CLI / embedder / roster health checks.

## Install

```bash
go get github.com/inovacc/agents
```

## Quick start

```go
import (
    "context"

    "github.com/inovacc/agents"
    _ "github.com/inovacc/agents/all" // register claude/codex/agy providers
)

func main() {
    // 1. Register the agents your app needs (the roster is yours).
    agents.Register(agents.Agent{
        Name:        "summarizer",
        Kind:        agents.KindResearch,
        Description: "Summarizes a document into 5 bullet points.",
        System:      "You are a terse summarizer. Emit exactly 5 bullets.",
    })

    // 2. Pick a provider backend and pair it with the roster.
    p, _ := agents.ProviderByName("claude") // or "codex" / "agy"
    ag := agents.NewAgency(p)

    // 3. Run an agent (warm session reused if the provider supports it).
    out, _ := ag.RunAgent(context.Background(), "summarizer", "…document…")
    _ = out
}
```

## Adding a provider

1. Create `<vendor>/<vendor>.go`, `package <vendor>`.
2. Return an `*agents.CLIProvider` preset (or a custom type implementing `agents.Provider`).
3. `init()`: `agents.RegisterProvider(func() agents.Provider { return New() }, "<name>", "<alias>")`.
4. Blank-import it from `all/all.go`.

> **Invariant:** the core never imports the vendor packages — they self-register.
> Import `github.com/inovacc/agents/all` (or a specific vendor package) wherever
> you call `ProviderByName`, or it errors "provider not registered".

## CLI

A thin binary lives at `cmd/agents` (health checks / version). Build with
`task build`.

## Build & test

```bash
task build      # go build ./...
task test       # fast tests
task lint       # golangci-lint
```

Single external dependency: `github.com/UserExistsError/conpty` (the Antigravity
ConPTY driver; Windows-only).

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright (c) 2026 inovacc.
