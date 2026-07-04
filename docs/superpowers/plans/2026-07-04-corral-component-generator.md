# corral Component Generator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** `corral gen` turns one canonical spec (`corral.json`) into a complete per-vendor component for Claude/Gemini/Codex — each an installable plugin tree PLUS a Go module wrapper (go.mod + corral wiring), sequa-style.

**Architecture:** New `aihost/` subpackage: a shared `Asset`/`Component` IR (`Load` from `corral.json`), a `Vendor` seam + `RegisterVendor` registry (claude/gemini/codex, each renders its plugin tree via `Plugin(*Component)`), a shared Go-module codegen (`text/template`+`go/format`, sequa's `renderFormatted`), and a `corral gen` CLI. `host/` is left untouched this milestone (generalization is a follow-on).

**Tech Stack:** Go stdlib only — `encoding/json`, `text/template`, `go/format`, `embed`. **Zero new dependencies** (native format is JSON to honor corral's one-dep thesis; YAML is an additive follow-on).

## Global Constraints
- Module `github.com/inovacc/corral`; new package `aihost` at `aihost/` (+ `aihost/vendors/{claude,gemini,codex}` + `aihost/vendors/all`).
- **Zero new deps.** Native spec = JSON (`encoding/json`); codegen = `text/template` + `go/format`.
- **Generated Go MUST be gofmt-clean + compile** — every Go render passes through `go/format.Source` (fail the render if it doesn't parse). A test compiles a generated component.
- **Frontmatter invariant:** every rendered asset `Frontmatter` ends with `\n` (the Render bug class from unravel's aihost) — golden-enforced.
- **Golden-file tests** per vendor (byte-compare; a `-update`-style regen).
- Vendor manifest formats (authoritative, from unravel's aihost hosts):
  - **Claude:** `commands/<n>.md`, `agents/<n>.md`, `skills/<n>/SKILL.md`, `hooks/hooks.json`, `.mcp.json`, `.claude-plugin/plugin.json`.
  - **Codex:** `.codex-plugin/plugin.json`, `skills/<n>/SKILL.md`, `.mcp.json`.
  - **Gemini:** `gemini-extension.json` (mcpServers inline), `skills/<n>/SKILL.md`, `GEMINI.md`.
- Commits: conventional, NO AI-attribution. Targeted `git add` (never `-A`).
- Consumed existing: `corral.Agent{Name,Kind,Description,Model,Tools,System,Schema}`.

---

## File Structure
- `aihost/ir.go` — `AssetKind`, `Asset`, `MCPSpec`, `Component`, `Load(path)`.
- `aihost/vendor.go` — `Vendor` interface, `RegisterVendor`/`Vendors`/`VendorByName`, `GeneratedFile`, `renderFormatted`, frontmatter helpers.
- `aihost/gen.go` — `Generate(c, vendors, out)`; the shared Go-module codegen (go.mod, agents.gen.go, component.gen.go, embed, doc.go, README) via templates; `WriteTree(files, base)` atomic writer.
- `aihost/vendors/{claude,gemini,codex}/vendor.go` — one `Vendor` impl each (its `Plugin(*Component)` tree).
- `aihost/vendors/all/all.go` — barrel blank-importing the three.
- `cmd/corral/gen.go` — `corral gen` cobra command.
- Tests: `aihost/ir_test.go`, `aihost/vendor_test.go`, `aihost/gen_test.go` (+ compile test), `aihost/vendors/*/vendor_test.go` (goldens), `cmd/corral/gen_test.go`.

---

## Task 1: IR + `Load`

**Files:** Create `aihost/ir.go`, `aihost/ir_test.go`

**Interfaces:**
- Produces: `AssetKind` (`"command"|"agent"|"subagent"|"skill"|"hook"`); `Asset{Kind,Name,Description,Body string; Metadata map[string]any}`; `MCPSpec{Command string; Args []string}`; `Component{Name,Module,Description string; MCP MCPSpec; Assets []Asset; Vendors []string}`; `func Load(path string) (*Component, error)`.

- [ ] **Step 1: Write the failing test**

Create `aihost/ir_test.go`:
```go
package aihost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ParsesComponentAndAssets(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "corral.json")
	os.WriteFile(cfg, []byte(`{
	  "component": {"name":"suite","module":"github.com/me/suite","description":"d",
	    "mcp":{"command":"corral","args":["mcp","serve"]}},
	  "vendors": ["claude","gemini","codex"],
	  "assets": [
	    {"kind":"agent","name":"researcher","description":"r","system":"do research","tools":["web"]},
	    {"kind":"command","name":"review","description":"rv","body":"# review"},
	    {"kind":"skill","name":"enrich","description":"en","body":"# enrich"}
	  ]
	}`), 0o644)

	c, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "suite" || c.Module != "github.com/me/suite" {
		t.Fatalf("component meta = %+v", *c)
	}
	if len(c.Vendors) != 3 || len(c.Assets) != 3 {
		t.Fatalf("vendors=%v assets=%d", c.Vendors, len(c.Assets))
	}
	if c.Assets[0].Kind != KindAgent || c.Assets[0].Name != "researcher" {
		t.Fatalf("asset[0] = %+v", c.Assets[0])
	}
	if c.MCP.Command != "corral" || len(c.MCP.Args) != 2 {
		t.Fatalf("mcp = %+v", c.MCP)
	}
}
```

- [ ] **Step 2: Run → fail** (`undefined: Load`). Run: `go test ./aihost/ -run TestLoad -v`.

- [ ] **Step 3: Create `aihost/ir.go`**

```go
/*
Copyright (c) 2026 inovacc
*/

// Package aihost is corral's cross-vendor component generator: it loads one
// canonical component spec (corral.json) into a vendor-neutral IR and generates,
// per AI vendor (Claude/Gemini/Codex), a complete component — an installable
// plugin tree plus a Go module wrapper. Codegen mirrors sequa's engine: only the
// per-vendor plugin tree varies (behind the Vendor interface); the IR and the
// Go-module codegen are shared.
package aihost

import (
	"encoding/json"
	"fmt"
	"os"
)

type AssetKind string

const (
	KindCommand  AssetKind = "command"
	KindAgent    AssetKind = "agent"
	KindSubagent AssetKind = "subagent"
	KindSkill    AssetKind = "skill"
	KindHook     AssetKind = "hook"
)

// Asset is a vendor-neutral definition of one command/agent/subagent/skill/hook.
type Asset struct {
	Kind        AssetKind      `json:"kind"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Body        string         `json:"body,omitempty"`   // prompt / instructions markdown
	System      string         `json:"system,omitempty"` // agents: system prompt (alias of Body)
	Tools       []string       `json:"tools,omitempty"`
	Model       string         `json:"model,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"` // argumentHint, allowedTools, event, matcher…
}

// PromptBody returns the effective prompt (Body, else System).
func (a Asset) PromptBody() string {
	if a.Body != "" {
		return a.Body
	}
	return a.System
}

type MCPSpec struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type Component struct {
	Name, Module, Description string
	MCP                       MCPSpec
	Assets                    []Asset
	Vendors                   []string
}

// spec is the on-disk corral.json shape.
type spec struct {
	Component struct {
		Name, Module, Description string  `json:"-"`
		MCP                       MCPSpec `json:"mcp"`
	} `json:"component"`
	Vendors []string `json:"vendors"`
	Assets  []Asset  `json:"assets"`
}

func Load(path string) (*Component, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("aihost: read %s: %w", path, err)
	}
	// component sub-object fields need explicit tags; decode into a shaped struct.
	var s struct {
		Component struct {
			Name        string  `json:"name"`
			Module      string  `json:"module"`
			Description string  `json:"description"`
			MCP         MCPSpec `json:"mcp"`
		} `json:"component"`
		Vendors []string `json:"vendors"`
		Assets  []Asset  `json:"assets"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("aihost: parse %s: %w", path, err)
	}
	if s.Component.Name == "" || s.Component.Module == "" {
		return nil, fmt.Errorf("aihost: component.name and component.module are required")
	}
	return &Component{
		Name: s.Component.Name, Module: s.Component.Module, Description: s.Component.Description,
		MCP: s.Component.MCP, Assets: s.Assets, Vendors: s.Vendors,
	}, nil
}
```
(Note: the `spec` type above is illustrative; use the inline `s` struct in `Load`. Remove the unused `spec` type to keep vet clean.)

- [ ] **Step 4: Run → pass.** Run: `go test ./aihost/ -run TestLoad -v && go build ./... && go vet ./aihost/`. `gofmt -w`; `gofmt -l` empty.

- [ ] **Step 5: Commit**
```bash
git add aihost/ir.go aihost/ir_test.go
git commit -m "feat(aihost): component IR + Load (corral.json spec)"
```

---

## Task 2: `Vendor` seam + registry + render helpers

**Files:** Create `aihost/vendor.go`, `aihost/vendor_test.go`

**Interfaces:**
- Consumes: `Component`, `Asset`.
- Produces: `type Vendor interface { Name() string; Plugin(c *Component) (map[string][]byte, error); InstallTarget(base string) (string, error) }`; `func RegisterVendor(f func() Vendor)`, `Vendors() []Vendor`, `VendorByName(string) (Vendor, bool)`; `type GeneratedFile struct{ Path string; Content []byte }`; `func renderFormatted(t *template.Template, data any) ([]byte, error)` (Go, via go/format); `func assetMarkdown(a Asset, frontmatter map[string]any) []byte` (frontmatter ends with `\n`).

- [ ] **Step 1: Write the failing test**

Create `aihost/vendor_test.go`:
```go
package aihost

import (
	"strings"
	"testing"
)

type fakeVendor struct{}

func (fakeVendor) Name() string { return "fake" }
func (fakeVendor) Plugin(*Component) (map[string][]byte, error) {
	return map[string][]byte{"x.md": []byte("x")}, nil
}
func (fakeVendor) InstallTarget(base string) (string, error) { return base + "/fake", nil }

func TestVendorRegistry(t *testing.T) {
	RegisterVendor(func() Vendor { return fakeVendor{} })
	if _, ok := VendorByName("fake"); !ok {
		t.Fatal("fake vendor not registered")
	}
	found := false
	for _, v := range Vendors() {
		if v.Name() == "fake" {
			found = true
		}
	}
	if !found {
		t.Fatal("fake not in Vendors()")
	}
}

func TestAssetMarkdown_FrontmatterEndsWithNewline(t *testing.T) {
	md := assetMarkdown(Asset{Name: "n", Description: "d", Body: "# body"}, map[string]any{"description": "d"})
	s := string(md)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatal("no opening frontmatter")
	}
	// The closing --- must be on its own line (frontmatter block ends with \n).
	if !strings.Contains(s, "\n---\n") {
		t.Fatalf("closing delimiter merged / missing:\n%s", s)
	}
}
```

- [ ] **Step 2: Run → fail.** Run: `go test ./aihost/ -run 'TestVendorRegistry|TestAssetMarkdown' -v`.

- [ ] **Step 3: Create `aihost/vendor.go`**

```go
/*
Copyright (c) 2026 inovacc
*/

package aihost

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strings"
	"sync"
	"text/template"
)

// Vendor is a codegen backend for one AI host (sequa's Engine). Only the
// installable plugin tree varies per vendor; the Go-module wrapper is shared.
type Vendor interface {
	Name() string
	Plugin(c *Component) (map[string][]byte, error) // slash-path → bytes
	InstallTarget(base string) (string, error)
}

var (
	vendorMu  sync.RWMutex
	vendorFac = map[string]func() Vendor{}
)

// RegisterVendor registers a vendor factory (call from init()). Unlike sequa's
// compiled-in switch, this is a registry so users add vendors without forking.
func RegisterVendor(f func() Vendor) {
	v := f()
	vendorMu.Lock()
	vendorFac[v.Name()] = f
	vendorMu.Unlock()
}

func Vendors() []Vendor {
	vendorMu.RLock()
	defer vendorMu.RUnlock()
	out := make([]Vendor, 0, len(vendorFac))
	for _, f := range vendorFac {
		out = append(out, f())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

func VendorByName(name string) (Vendor, bool) {
	vendorMu.RLock()
	f, ok := vendorFac[name]
	vendorMu.RUnlock()
	if !ok {
		return nil, false
	}
	return f(), true
}

// GeneratedFile is one file the caller writes to disk.
type GeneratedFile struct {
	Path    string
	Content []byte
}

// renderFormatted executes a Go-source template and runs it through go/format —
// so generated Go is always gofmt-clean and syntactically valid (sequa's guard).
func renderFormatted(t *template.Template, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("aihost: template %s: %w", t.Name(), err)
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("aihost: gofmt generated %s: %w\n%s", t.Name(), err, buf.String())
	}
	return out, nil
}

// assetMarkdown renders an Asset to a vendor markdown file: YAML frontmatter
// (from the provided fields, in stable key order) + blank line + body. The
// frontmatter block ALWAYS ends with a newline so the closing --- is on its own
// line (the Render invariant carried from unravel's aihost).
func assetMarkdown(a Asset, frontmatter map[string]any) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	keys := make([]string, 0, len(frontmatter))
	for k := range frontmatter {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, yamlScalar(frontmatter[k]))
	}
	b.WriteString("---\n\n")
	b.WriteString(a.PromptBody())
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// yamlScalar renders a frontmatter value as a minimal YAML scalar (string,
// []string as a flow list, else fmt).
func yamlScalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []string:
		return "[" + strings.Join(t, ", ") + "]"
	default:
		return fmt.Sprint(t)
	}
}
```

- [ ] **Step 4: Run → pass.** `go test ./aihost/ -run 'TestVendorRegistry|TestAssetMarkdown' -v && go build ./... && go vet ./aihost/`. gofmt clean.

- [ ] **Step 5: Commit**
```bash
git add aihost/vendor.go aihost/vendor_test.go
git commit -m "feat(aihost): Vendor seam + RegisterVendor registry + render helpers"
```

---

## Task 3: Claude vendor (`aihost/vendors/claude`) + golden

**Files:** Create `aihost/vendors/claude/vendor.go`, `aihost/vendors/claude/vendor_test.go`

**Interfaces:** Produces a `Vendor` whose `Plugin` emits the Claude tree.

- [ ] **Step 1: Write the golden test**

Create `aihost/vendors/claude/vendor_test.go` — build a small `*aihost.Component`, call `Host{}.Plugin(c)`, and assert the expected paths exist with a content spot-check + the frontmatter-`\n` invariant:
```go
package claude

import (
	"strings"
	"testing"

	"github.com/inovacc/corral/aihost"
)

func sampleComponent() *aihost.Component {
	return &aihost.Component{
		Name: "suite", Module: "github.com/me/suite",
		MCP: aihost.MCPSpec{Command: "corral", Args: []string{"mcp", "serve"}},
		Assets: []aihost.Asset{
			{Kind: aihost.KindCommand, Name: "review", Description: "rv", Body: "# /review"},
			{Kind: aihost.KindAgent, Name: "researcher", Description: "r", System: "do research"},
			{Kind: aihost.KindSkill, Name: "enrich", Description: "en", Body: "# enrich"},
			{Kind: aihost.KindHook, Name: "on-stop", Description: "h",
				Metadata: map[string]any{"event": "Stop", "command": "corral hook stop"}},
		},
	}
}

func TestClaudePlugin_EmitsFullTree(t *testing.T) {
	tree, err := (Host{}).Plugin(sampleComponent())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"commands/review.md", "agents/researcher.md", "skills/enrich/SKILL.md",
		"hooks/hooks.json", ".mcp.json", ".claude-plugin/plugin.json",
	} {
		if _, ok := tree[want]; !ok {
			t.Errorf("claude tree missing %s", want)
		}
	}
	// frontmatter-\n invariant on a rendered md asset.
	if md := string(tree["agents/researcher.md"]); !strings.Contains(md, "\n---\n") {
		t.Errorf("agent md frontmatter malformed:\n%s", md)
	}
	// hooks.json wires the Stop hook.
	if !strings.Contains(string(tree["hooks/hooks.json"]), "corral hook stop") {
		t.Errorf("hooks.json missing Stop command")
	}
}
```

- [ ] **Step 2: Run → fail** (`undefined: Host`). `go test ./aihost/vendors/claude/ -v`.

- [ ] **Step 3: Implement `aihost/vendors/claude/vendor.go`**

`Host` implements `aihost.Vendor`. `Plugin(c)`:
- For each asset: command → `commands/<name>.md` via `assetMarkdown` (frontmatter `description`, `argument-hint`, `allowed-tools` from Metadata; NO `name` key); agent/subagent → `agents/<name>.md` (frontmatter `name`,`description`); skill → `skills/<name>/SKILL.md` (frontmatter `name`,`description`).
- Hooks → aggregate all `KindHook` assets into `hooks/hooks.json`: `{"hooks":{<event>:[{"matcher":<matcher?>,"hooks":[{"type":"command","command":<command>}]}]}}` (matcher omitted when absent).
- `.mcp.json`: `{"mcpServers":{<c.Name>:{"command":c.MCP.Command,"args":c.MCP.Args}}}`.
- `.claude-plugin/plugin.json`: `{"name":c.Name,"version":"0.1.0","description":c.Description,"author":{"name":"…"},"license":"BSD-3-Clause","commands":"commands","agents":"agents","skills":"skills","mcpServers":".mcp.json"}`.
- `InstallTarget(base)` → `filepath.Join(base, ".claude", "plugins", "marketplaces", c-independent name)` — use `~/.claude/plugins/marketplaces/<component>/` (component name passed via a field or default; simplest: `filepath.Join(base, ".claude", "plugins", "marketplaces")` + component-name is added by the caller). Keep it: `InstallTarget(base) => filepath.Join(base, ".claude", "plugins", "marketplaces")`.
Use the existing `host` package's JSON marshaling style (`json.MarshalIndent`, trailing `\n`). Register via `func init(){ aihost.RegisterVendor(func() aihost.Vendor { return Host{} }) }`.

- [ ] **Step 4: Run → pass.** `go test ./aihost/vendors/claude/ -v && go build ./...`. gofmt clean.

- [ ] **Step 5: Commit**
```bash
git add aihost/vendors/claude/
git commit -m "feat(aihost): claude vendor — full plugin tree from component IR"
```

---

## Task 4: Gemini vendor (`aihost/vendors/gemini`) + golden

Same structure as Task 3; the **Gemini tree**:
- `skills/<name>/SKILL.md` for skill assets (frontmatter `name`,`description`, ending `\n`).
- Commands/agents surfaced as a portable **`skills/<component>-command-library/SKILL.md`** + `…-agent-library/SKILL.md` (index of command/agent assets — lensr `portable_libraries` pattern) since Gemini has no native command/agent surface.
- `gemini-extension.json`: `{"name":c.Name,"version":"0.1.0","description":c.Description,"contextFileName":"GEMINI.md","mcpServers":{<c.Name>:{"command":c.MCP.Command,"args":c.MCP.Args}}}`.
- `GEMINI.md`: a short context file (component description + a note on the bundled skills).
- No hooks (Gemini has no hook surface — hook assets are skipped, logged).
- `InstallTarget(base)` → `filepath.Join(base, ".gemini", "extensions", c.Name)`.
Test asserts `gemini-extension.json`, `GEMINI.md`, `skills/enrich/SKILL.md`, and the two library skills exist. Register in `init()`. Commit `feat(aihost): gemini vendor — extension tree + portable command/agent libraries`.

## Task 5: Codex vendor (`aihost/vendors/codex`) + golden

Same structure; the **Codex tree**:
- `skills/<name>/SKILL.md` for skills; commands/agents via the same portable-library skills as Gemini.
- `.codex-plugin/plugin.json`: `{"name":c.Name,"version":"0.1.0","description":c.Description,"author":{"name":"…"},"license":"BSD-3-Clause","skills":"skills","mcpServers":".mcp.json"}`.
- `.mcp.json`: same as Claude's.
- No hooks. `InstallTarget(base)` → `filepath.Join(base, ".codex", "plugins", c.Name)`.
Test asserts the three paths + library skills. Register in `init()`. Then create `aihost/vendors/all/all.go` blank-importing `claude`,`gemini`,`codex`. Commit `feat(aihost): codex vendor + vendors/all barrel`.

---

## Task 6: Go-module codegen + `Generate` + compile test

**Files:** Create `aihost/gen.go`, `aihost/gen_test.go`

**Interfaces:** Produces `func Generate(c *Component, vendors []string, out string) ([]GeneratedFile, error)`; `func WriteTree(files []GeneratedFile, base string) (int, error)` (atomic tmp+rename).

`Generate` — for each vendor: get `VendorByName`, call `Plugin(c)` → plugin-tree files under `<out>/<vendor>/assets/…`, then the shared Go-module wrapper under `<out>/<vendor>/`:
- `go.mod` (template): `module <c.Module>/<vendor>` + `go 1.25` + `require github.com/inovacc/corral <ver>` (version from a const, e.g. `corralModuleVersion = "v0.1.1"`).
- `agents.gen.go` (renderFormatted): the `KindAgent`/`KindSubagent` assets as `corral.Agent{...}` literals + `func init(){ corral.Register(func() corral.Agent { return … }) }` per agent. Header `// Code generated by corral. DO NOT EDIT.`
- `component.gen.go` (renderFormatted): `func New() (*corral.Agency, error) { return corral.NewAgency("<vendor>", ".") }` + `//go:embed assets` var + `func Install(base string) (int, error)` that walks the embedded FS and writes it to the vendor install dir.
- `doc.go` (renderFormatted): package doc. `README.md`: usage.

`gen_test.go`:
- Golden the `go.mod`/`*.gen.go` for one vendor from `sampleComponent()`.
- **Compile test:** write a generated component to `t.TempDir()`, then `go vet`/`go build` it (skip if `go` toolchain absent) — proving the output is valid, buildable Go (sequa's guarantee). At minimum, assert each `*.gen.go` parses (`go/parser.ParseFile`).

- [ ] Steps: RED (undefined Generate) → implement templates + Generate + WriteTree → GREEN (golden + parse/compile). Commit `feat(aihost): Go-module codegen — Generate complete per-vendor component`.

---

## Task 7: `corral gen` CLI

**Files:** Create `cmd/corral/gen.go`; Modify `cmd/corral/main.go`

`newGenCmd()`: `corral gen [--config corral.json] [--out ./gen] [--vendor all] [--dry-run]`:
- `aihost.Load(config)` → Component; select vendors (`--vendor` override else `c.Vendors`); `aihost.Generate(c, vendors, out)`; if `--dry-run` print planned paths, else `aihost.WriteTree(files, out)` and print a summary.
- `corral gen list --config` → print IR (assets by kind, target vendors).
- Blank-import `_ "github.com/inovacc/corral/aihost/vendors/all"` so the three vendors register.
- Wire into `cmd/corral/main.go`: add `newGenCmd()` to `root.AddCommand(...)` (and to the light-command path if `gen` should bypass mantle — it should, like usage/serve: add `"gen"` to `isLightCommand`).

`gen_test.go`: `gen --dry-run` against a fixture `corral.json` lists the expected per-vendor files for all three vendors; `gen list` prints the asset counts.

- [ ] Steps: RED → implement → GREEN (`go run ./cmd/corral gen --help`, dry-run smoke, full suite) → commit `feat(cmd): corral gen — generate per-vendor components from corral.json`.

---

## Self-Review Notes
- **Spec coverage:** §2 canonical spec → Task 1 `Load`; §3 IR → Task 1; §4 Vendor+registry → Task 2; §4 vendor trees → Tasks 3-5; §5 Go-module component → Task 6; §6 CLI → Task 7; §8 testing (goldens, compile test, frontmatter-`\n`, dry-run) → across tasks. Native format = JSON (zero-dep) per the "native first"/one-dep constraint; YAML is a noted follow-on.
- **Type consistency:** `Component`/`Asset`/`AssetKind`/`MCPSpec` (Task 1) consumed by every later task; `Vendor`/`RegisterVendor`/`GeneratedFile`/`renderFormatted`/`assetMarkdown` (Task 2) by Tasks 3-6; `Generate`/`WriteTree` (Task 6) by Task 7.
- **Non-goals honored:** no multi-format import (JSON native only), no runtime execution (that's the workflow-composition plan), `host/` untouched (generalization deferred), zero new deps.
- **Deferred detail resolved:** native format = JSON (stdlib) — protects corral's one-dep thesis; YAML importer is an additive follow-on.
