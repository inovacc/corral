# corral

<!-- rev:002 -->

[![Go Reference](https://pkg.go.dev/badge/github.com/inovacc/corral.svg)](https://pkg.go.dev/github.com/inovacc/corral)
[![Test](https://github.com/inovacc/corral/actions/workflows/test.yml/badge.svg)](https://github.com/inovacc/corral/actions/workflows/test.yml)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue.svg)](LICENSE)
[![Go 1.26](https://img.shields.io/badge/Go-1.26-00ADD8.svg)](https://go.dev/)

> A provider-abstracted runtime for driving **subscription coding-agent CLIs**
> (Claude Code, Codex, Antigravity, Grok, Kimi, Qwen) behind one interface —
> with a warm session pool, rate-limit awareness, and a per-host plugin
> installer. Built on [mantle](https://github.com/inovacc/mantle).

`corral` runs a roster of **declarative agents** through a pluggable **Provider**
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
| `grok/`  | **x.ai Grok**          | headless `grok --single` (mirrors Claude Code flags) |
| `kimi/`  | **Moonshot Kimi Code** | headless `kimi --prompt` (auto-approve via `--yolo`) |
| `qwen/`  | **Alibaba Qwen Code**  | headless `qwen --prompt` (gemini-cli fork; needs `--auth-type`) |
| `all/`   | —                      | blank-imports every provider so they self-register |
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
go get github.com/inovacc/corral
```

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/inovacc/corral"
    _ "github.com/inovacc/corral/all" // register every provider (claude/codex/agy/grok/kimi/qwen)
)

func main() {
    // 1. Register the agents your app needs. Register takes a lazy factory;
    //    the roster is yours — the library ships no built-in agents.
    corral.Register(func() corral.Agent {
        return corral.Agent{
            Name:        "summarizer",
            Kind:        corral.KindResearch,
            Description: "Summarizes a document into 5 bullet points.",
            System:      "You are a terse summarizer. Emit exactly 5 bullets.",
        }
    })

    // 2. Open an Agency for a provider backend (by name) + a working dir.
    ag, err := corral.NewAgency("claude", ".") // or codex / agy / grok / kimi / qwen
    if err != nil {
        log.Fatal(err)
    }
    defer ag.Close()

    // 3. Look the agent up by name and run it (warm session reused if supported).
    agent, _ := corral.ByName("summarizer")
    out, err := ag.RunAgent(context.Background(), agent, "…document…")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(out.Text)
}
```

## Adding a provider

1. Create `<vendor>/<vendor>.go`, `package <vendor>`.
2. Return an `*corral.CLIProvider` preset (or a custom type implementing `corral.Provider`).
3. `init()`: `corral.RegisterProvider(func() corral.Provider { return New() }, "<name>", "<alias>")`.
4. Blank-import it from `all/all.go`.

> **Invariant:** the core never imports the vendor packages — they self-register.
> Import `github.com/inovacc/corral/all` (or a specific vendor package) wherever
> you call `ProviderByName`, or it errors "provider not registered".

## CLI

A thin binary lives at `cmd/corral` (health checks / version). Build with
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
