# corral Component Generator — canonical spec → per-vendor component (definition plane) — Design

**Status:** Approved to plan · 2026-07-04 · module `github.com/inovacc/corral`
**Supersedes** the earlier "multi-format import → vendor render" framing of this file. Reframed after mapping **sequa** (`the-migrator`): corral is a **codegen engine** in sequa's image, not a document renderer.

## 0. Vision (the sequa mapping)

sequa takes one canonical **SQL** and generates a dialect-specific Go **component** for postgres/sqlite/mysql. corral takes one canonical **agent spec** and generates a vendor-specific **component** for the AI vendors — **Claude = postgres, Gemini = sqlite, Codex = mysql**. Per the agent's needs, `corral gen` produces a **complete, tailor-made component per vendor**: the vendor's installable plugin tree **and** a Go module wrapper (go.mod + corral wiring) — "the import module and all the things needed to work" — so the component is both **installable** into that CLI and **runnable/importable** as Go.

sequa's proven pattern, adopted deliberately:
- A small **`Vendor` interface** (= sequa's `Engine`/dialect) — only the vendor-specific translation lives behind it; the IR and the Go-module codegen are **shared**.
- **Config-driven** (a `corral.yaml`, = sequa.yaml), not flag-soup.
- Codegen via **`text/template` + `go/format`** (gofmt-clean, valid Go — exactly sequa's `renderFormatted`).
- **Golden-file tests** (byte-compare generated trees — sequa's safety net).
- **Native canonical spec** as the single input (= sequa's SQL; matches the "native format first" decision).
- **One deliberate improvement over sequa:** a `RegisterVendor` **registry** (sequa hardcodes a `switch` — a limitation we do NOT copy, because corral is "a module for extensibility").

## 1. Architecture

```
  corral.yaml + native asset spec (YAML/JSON)        SHARED IR                 PER-VENDOR + SHARED CODEGEN                 COMPLETE COMPONENT
  component{name,module,mcp} + assets[              Component{Meta,        ┌─ Vendor.Plugin(c) → plugin tree          ┌─ <out>/<vendor>/
   {kind:agent|command|subagent|skill|hook,   ──▶  []Asset{Kind,Name,  ──▶│  (claude/gemini/codex manifests+md)   ──▶ │   plugin tree (installable)
    name,description,model,tools,system,...}]        Description,Body,     └─ shared Go-module codegen (go.mod,       │   go.mod + *.gen.go + assets/ (runnable)
                                                     Metadata}}               agents.gen.go, component.gen.go,          │   doc.go + README.md
       PARSE (native, stdlib/1-dep)                  [source of truth]        embed, doc.go) parameterized by vendor    └─ (installable AND importable)
```

- **Per-vendor (behind `Vendor`):** the plugin-tree shape + manifest formats (Claude vs Gemini vs Codex). *This is the only thing that varies* — sequa's insight (only parse+typemap vary; render is shared).
- **Shared:** the IR, the Go-module wrapper codegen, the CLI, install, and golden harness.

## 2. Canonical input — `corral.yaml` + native asset spec

```yaml
version: "1"
component:
  name: my-agent-suite
  module: github.com/me/my-agent-suite     # → generated go.mod module path
  description: "…"
  mcp: { command: corral, args: [mcp, serve] }   # the component's MCP server wiring
vendors: [claude, gemini, codex]               # which components to generate
assets:                                        # inline, OR `assetDir: ./assets` of YAML/JSON files
  - { kind: agent,   name: researcher, description: "…", model: "", tools: [web], system: "…" }
  - { kind: command, name: review,     description: "…", argumentHint: "[path]", allowedTools: [Read], body: "…" }
  - { kind: skill,   name: enrich,     description: "…", body: "…" }
  - { kind: subagent, name: verifier,  description: "…", system: "…" }
  - { kind: hook,    name: on-stop,    description: "…", event: Stop, command: "corral hook stop" }  # claude-only
```
Native format only this milestone (JSON via stdlib; YAML via a single lean dep — the "native format first" decision). Multi-format IMPORT (Claude-md/Gemini-toml → spec) is a deferred, additive follow-on (its own `Importer` registry), NOT in this milestone.

## 3. Shared IR (`aihost/ir.go`, package `aihost`)

```go
type AssetKind string // "command" | "agent" | "subagent" | "skill" | "hook"
type Asset struct {
	Kind        AssetKind
	Name        string
	Description string
	Body        string
	Metadata    map[string]any // tools, argumentHint, allowedTools, model, event, matcher…
}
type Component struct {
	Name, Module, Description string
	MCP    MCPSpec            // command + args
	Assets []Asset
}
func Load(configPath string) (*Component, []string /*vendors*/, error) // parse corral.yaml (+assetDir)
```

## 4. The `Vendor` seam + registry (`aihost/vendor.go`)

```go
// Vendor is a codegen backend for one AI host (sequa's Engine). Only the
// installable plugin tree varies per vendor; the Go-module wrapper is shared.
type Vendor interface {
	Name() string                                     // "claude" | "gemini" | "codex"
	Plugin(c *Component) (map[string][]byte, error)   // slash-path → bytes: the installable tree
}
func RegisterVendor(f func() Vendor)                  // lazy factory (corral idiom); NOT a switch
func Vendors() []Vendor
func VendorByName(name string) (Vendor, bool)
```
Built-in vendors (`aihost/vendors/{claude,gemini,codex}`), registered in `init()`, barreled via `aihost/vendors/all`. Reuse the manifest formats already proven in unravel/lensr aihost (Claude: commands/agents/skills/`hooks/hooks.json`/`.mcp.json`/`.claude-plugin/plugin.json`; Gemini: `gemini-extension.json`+skills+`GEMINI.md`; Codex: `.codex-plugin/plugin.json`+skills+`.mcp.json`). Commands/agents that a vendor can't express natively are surfaced as portable library skills (lensr's `portable_libraries` pattern). Enforce the **frontmatter-ends-with-`\n`** invariant (unravel's Render bug class) via a golden.

## 5. The generated component — a complete unit (`aihost/gen.go`)

For each selected vendor, `Generate` emits `<out>/<vendor>/` containing:

**(a) the installable plugin tree** — from `Vendor.Plugin(c)` (markdown assets + manifests).

**(b) the Go module wrapper** (sequa-style codegen; `text/template`+`go/format`, auto-computed imports):
- `go.mod` — `module <component.Module>/<vendor>` + `go <directive>` (mirrors corral's OWN go.mod `go` directive, not an independently chosen version — a generated module requiring corral must declare at least the Go version corral itself was built with) + `require github.com/inovacc/corral <ver>`.
- `agents.gen.go` — the spec's agent/subagent assets as `corral.Agent` literals + `func init(){ corral.Register(...) }` (importing the module registers the roster).
- `component.gen.go` — `New(provider string) (*corral.Agency, error)` calling `corral.NewAgency(provider, ".")`: the runtime provider is the caller's choice, since corral registers claude/codex/agy/grok/kimi (no gemini yet) and this component may have been generated for a vendor corral can't run as a provider. Also emits `Install(base string) (int, error)` that writes the embedded plugin tree under `base`.
- `assets_embed.gen.go` — `//go:embed all:assets` of the plugin tree (the `all:` prefix is required so dot-prefixed files like `.mcp.json` and `.claude-plugin/plugin.json` aren't silently excluded), so the component installs itself.
- `doc.go` (package doc, provenance `// Code generated by corral. DO NOT EDIT.`) + `README.md` (usage).

So the component is **installable** (`Install()` writes the vendor plugin) **and runnable** (`New(provider)` returns an Agency for whichever provider the caller picks) — depends only on stdlib + corral.

```go
func Generate(c *Component, vendors []string) ([]GeneratedFile, error)
type GeneratedFile struct { Path string; Content []byte } // caller writes atomically
```

## 6. CLI (`cmd/corral/gen.go`)

Config-driven like sequa (`--config`, no per-vendor flag-soup); a `--vendor` override for one-offs:
- `corral gen [--config corral.yaml] [--out ./gen] [--vendor claude|gemini|codex|all] [--dry-run]` — parse → IR → per-vendor Generate → write atomically. Dry-run lists planned files.
- `corral gen list` — show the loaded IR (assets by kind, target vendors).
- `corral gen doctor --vendor <v>` — reuse the existing `host` Doctor/Report health model.

## 7. Constraints & non-goals

- **Deps:** native spec parse = stdlib JSON + one lean pure-Go YAML dep (or hand-rolled minimal YAML, decided at plan time); Go codegen = **stdlib** (`text/template`, `go/format`) — sequa's exact approach, **no heavy deps, no adk-go**. Runtime/provider/serve paths add nothing.
- **Generated Go must be `gofmt`-clean and compile** — every render goes through `go/format` (sequa's `renderFormatted`); a test compiles a generated component.
- **Golden tests** per vendor (byte-compare the generated tree; `-update` regenerates) — sequa's safety net.
- **Extensibility:** `RegisterVendor` (+ a later `RegisterImporter`) is the module contract; `docs/EXTENDING.md` documents adding a vendor.
- **Non-goals (this milestone):** multi-format IMPORT (native spec only now); the *runtime execution/orchestration* of agents (covered by the workflow-composition spec — the execution plane); round-trip fidelity; a standalone-monorepo generator (each vendor component gets its own `go.mod` sub-path, not a separate repo).
- **Relationship to `corral.Agent`:** an `Asset{Kind:agent}` ⇄ `corral.Agent` mapping seeds `agents.gen.go`; the two types stay distinct (definition IR vs runtime unit).
- **Relationship to existing `host/`:** `host/`'s atomic `Install`, `Host` registry, `Doctor`/`Report`, and `AgentMarkdown`/`MCPManifest` are the seed of the Claude `Vendor` + install layer; `host/` is generalized into `aihost/` and left a thin deprecated shim (≥30-day) until the CLI cuts over.

## 8. Testing
- IR/`Load`: parse a `corral.yaml` fixture → expected `Component`.
- Each `Vendor.Plugin`: `Component` → expected tree (paths + content spot-check + frontmatter-`\n`).
- Go-module codegen: golden `*.gen.go` + a test that the generated module **compiles** (`go build` a fixture output in a temp dir) — the sequa "output is valid Go" guarantee.
- Install: `t.TempDir()` atomic write + stale-sweep.
- CLI: `gen --dry-run` + `gen list` against a fixture; all three vendors.
- Network-free; `task test` + `task lint` green.
