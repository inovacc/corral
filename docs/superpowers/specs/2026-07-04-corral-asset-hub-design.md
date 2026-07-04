# corral Asset Hub — single source of truth, multi-format import → vendor render (definition plane) — Design

**Status:** Approved to plan · 2026-07-04 · module `github.com/inovacc/corral`

## 0. Problem

corral should be the **single source of truth** for AI agent definitions — commands, agents, subagents, skills, hooks — and a **module for extensibility**: users bring their own definitions in whatever format they already have, and corral renders + installs them into each vendor's document format (Claude plugin, Gemini extension, Codex plugin, …). Today corral has only a thin seed: `host/` renders the runtime `corral.Agent` roster (inline Go structs) to a **byte-identical** `agents/<name>.md` + `.mcp.json` tree for claude/codex/agy — **no `Asset` IR, no importer, no vendor-specific rendering, no assets CLI** (the render code is library-only, unwired). This builds the full **import → canonical IR → vendor render** pipeline, generalizing `host/` and bringing lensr's `aihost` maturity, so corral becomes the cross-vendor "define once, deploy anywhere" hub.

## 1. Architecture

```
  IMPORTERS (pluggable)         CANONICAL IR (source of truth)      RENDERERS (pluggable)
  Claude .md (frontmatter) ─┐                                    ┌─ Claude   → commands/ agents/ skills/ hooks.json .mcp.json .claude-plugin/plugin.json
  Gemini .toml ─────────────┤   Asset{ Kind, Name, Description,  ┼─ Gemini   → gemini-extension.json + skills/ + GEMINI.md
  generic YAML / JSON ──────┼─▶  Body, Metadata, Source }  ──────┼─ Codex    → .codex-plugin/plugin.json + skills/ + .mcp.json
  «user-registered»  ───────┘   held in a Library (all/by-kind)   └─ «user-registered»
```

- **Canonical IR (`Asset`)** — the vendor-neutral definition model, distinct from the runtime `corral.Agent` (which stays the execution unit). An `Asset` of `KindAgent` can be derived from / to a `corral.Agent`; the IR is broader (also command/subagent/skill/hook).
- **Library** — the in-memory single-source-of-truth collection assets are imported into and rendered from.
- **Importer / Renderer** — pluggable interfaces (lazy-factory registries, mirroring corral's existing `host.register` / `RegisterProvider` idiom) so users extend both the input and output sides.
- **Generalize `host/`, don't duplicate:** its atomic `Install`, `Host` registry, and `Doctor`/`Report` become the install/health layer; `AgentMarkdown`/`MCPManifest`/`sharedFiles` become the seed of the **Claude renderer**. The new home is package **`aihost/`** (mirrors the lensr lineage the user named); `host/` is migrated in and left as a thin deprecated shim (≥30-day window per the deprecation policy) or removed in a dedicated commit once the CLI cuts over.

## 2. The IR (`aihost/asset.go`)

```go
type AssetKind string // "command" | "agent" | "subagent" | "skill" | "hook"

type Asset struct {
	Kind        AssetKind
	Name        string         // unique within kind (kebab-case)
	Description string
	Body        string         // the prompt / instructions (markdown)
	Metadata    map[string]any // vendor-neutral extras: tools, argument-hint, model, allowed-tools, trigger, matcher…
	Source      string         // provenance: origin file path / importer name
}

type Library struct { /* assets, indexed by (kind,name) */ }
func NewLibrary() *Library
func (l *Library) Add(a ...Asset) error            // dup (kind,name) is an error
func (l *Library) All() []Asset                    // sorted, stable
func (l *Library) ByKind(k AssetKind) []Asset
func (l *Library) ByName(k AssetKind, name string) (Asset, bool)
```

## 3. Importers (`aihost/importer.go` + `aihost/importers/*`)

```go
type Importer interface {
	Name() string
	CanImport(path string) bool            // by extension + a cheap content sniff
	Import(path string) ([]Asset, error)   // one file → assets
}
func RegisterImporter(f func() Importer)
func Importers() []Importer
// ImportDir walks dir, dispatches each file to the first Importer that CanImport it,
// collecting into a Library; unknown files are skipped (reported).
func ImportDir(dir string) (*Library, []string /*skipped*/, error)
```

**Broad first-milestone importers:**
- **`claudemd`** — Claude `.md` with YAML frontmatter. Kind inferred from the containing dir (`commands/`→command, `agents/`→agent, `skills/…/SKILL.md`→skill) or a frontmatter `kind:`. Frontmatter → `Metadata` (description/argument-hint/allowed-tools/name), body → `Body`.
- **`geminitoml`** — Gemini command `.toml` (name/description/prompt) → command Assets.
- **`corralyaml`** — a native corral YAML **and** JSON asset spec (a list of `{kind,name,description,body,metadata}`), the canonical authoring format. JSON via stdlib `encoding/json`.

**Dependency decision (flag in review):** parsing needs a YAML reader (frontmatter + `corralyaml`) and a TOML reader (`geminitoml`). corral's thesis is "one external dep (`conpty`)". Options: **(A) accept two lean pure-Go parsers** (`gopkg.in/yaml.v3`, `github.com/BurntSushi/toml`) confined to the `aihost` subsystem (runtime stays lean); **(B) hand-roll a minimal `key: value` + block-scalar frontmatter parser** (avoids YAML for the common Claude-md case) and defer TOML. **Recommendation: (A)** — the asset hub is an explicit new subsystem; two standard parsers are proportionate, and the runtime/provider path keeps its lean budget. Confirm at spec review.

## 4. Renderers (`aihost/renderer.go` + `aihost/renderers/*`)

```go
type Renderer interface {
	Vendor() string                                       // "claude" | "gemini" | "codex"
	Render(lib *Library, td TemplateData) (map[string][]byte, error) // slash-path → bytes
}
func RegisterRenderer(f func() Renderer)
func Renderers() []Renderer
func RendererByVendor(v string) (Renderer, bool)
type TemplateData struct { Name, Version, Description, McpCommand string }
```

**Broad first-milestone renderers (formats confirmed from the unravel/lensr aihost hosts):**
- **`claude`** — `commands/<n>.md`, `agents/<n>.md`, `skills/<n>/SKILL.md`, `hooks/hooks.json` (from hook assets), `.mcp.json`, `.claude-plugin/plugin.json`. (Generalizes `host.AgentMarkdown`/`MCPManifest` to all kinds.)
- **`gemini`** — `gemini-extension.json` (mcpServers inline) + `skills/<n>/SKILL.md` + `GEMINI.md`. Commands are TOML on Gemini — render command assets into a portable command-library skill (the lensr `portable_libraries` pattern) so they stay discoverable.
- **`codex`** — `.codex-plugin/plugin.json` + `skills/<n>/SKILL.md` + `.mcp.json`; commands/agents likewise surfaced via portable-library skills.

Rendering an `Asset` to a vendor markdown file = frontmatter (from `Kind` + `Metadata`, per that vendor's convention) + `Body`, with the **frontmatter-ends-with-`\n`** invariant enforced (the Render defect class from unravel's `pkg/aihost`).

## 5. Install + CLI (`aihost/install.go`, `cmd/corral/assets.go`)

- **Install:** generalize `host.Install` — `Install(r Renderer, lib *Library, target string, dryRun bool) (Result, error)`, atomic tmp+rename + stale-sweep (adopt lensr's `WriteTreeAtomic`).
- **CLI (`corral assets …`)** — the user-facing extensibility surface:
  - `corral assets import <dir>` — load definitions → Library; print a summary (counts by kind, skipped files).
  - `corral assets list` — show the loaded IR.
  - `corral assets render --from <dir> --vendor claude --out <dir>` — import + render to disk.
  - `corral assets install --from <dir> --vendor claude|gemini|codex|all [--dry-run]` — import + render + install into the host.
  - `corral assets doctor --vendor <v>` — reuse the `host` Doctor/Report health model.

## 6. Extensibility (the module contract)

corral is imported as a Go module; downstream code registers its own `Importer`/`Renderer` in `init()` (same lazy-factory pattern as providers). A `docs/aihost/EXTENDING.md` documents: implement the interface, `RegisterImporter`/`RegisterRenderer`, and your format/vendor joins the pipeline. The built-in importers/renderers ship in `aihost/importers` / `aihost/renderers` barrels.

## 7. Constraints & non-goals

- **Dep budget:** +2 parser deps (yaml.v3, toml) confined to `aihost` (pending review); the runtime/provider/serve paths add none.
- **No embed.FS-only limitation:** unlike unravel's aihost (Go-literals only), corral's hub **loads real files** (that's the point) — it adopts lensr's `toolkit/` real-file model.
- **Frontmatter-`\n` invariant** carried over from unravel's aihost (guarded by a render test).
- **Non-goals (this milestone):** round-trip fidelity guarantees (importing then re-rendering to the SAME source format byte-for-byte); a GUI; remote/registry asset sources; the runtime execution of assets (that's the workflow-composition/execution-plane spec). Hooks rendering is Claude-only (codex/gemini have no hook surface).
- **Relationship to the runtime `Agent`:** an `Asset{Kind:agent}` ⇄ `corral.Agent` mapping is provided so the existing roster can seed the Library and vice-versa, but the two types stay distinct (definition IR vs execution unit).

## 8. Testing

- IR/Library: add/dup/index table tests.
- Each importer: parse a fixture file → expected Assets (frontmatter/metadata/body mapping; kind inference).
- Each renderer: Library → expected vendor tree (paths + a content spot-check + the frontmatter-`\n` invariant).
- Install: `t.TempDir()` atomic write + stale-sweep.
- CLI: `assets import`/`list` against a fixture dir; `render`/`install --dry-run` smoke.
- All network-free; `task test` green; `task lint` clean.
