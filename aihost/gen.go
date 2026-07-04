/*
Copyright (c) 2026 inovacc
*/

package aihost

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// corralModuleVersion pins the github.com/inovacc/corral require line emitted
// into every generated vendor go.mod.
const corralModuleVersion = "v0.1.1"

// generatedGoDirective pins the `go` directive emitted into every generated
// vendor go.mod. It mirrors corral's OWN go.mod `go` directive (read via
// `grep '^go ' go.mod` in the corral repo root) rather than an independently
// chosen version, since a generated module requiring corral must declare a Go
// version at least as new as the one corral itself was built with.
const generatedGoDirective = "1.26.3"

// wrapperPackage is the fixed package name used by every generated Go-module
// wrapper file (agents.gen.go, component.gen.go, doc.go). Each vendor gets its
// own go.mod (module <c.Module>/<vendor>), so a single shared package name
// across vendors causes no collision — picked for simplicity over deriving a
// per-vendor identifier.
const wrapperPackage = "component"

// Generate renders, for each named vendor, the vendor's installable plugin
// tree (under "<vendor>/assets/…") plus a shared Go-module wrapper
// (go.mod, agents.gen.go, component.gen.go, doc.go, README.md) under
// "<vendor>/". Returned paths are relative (slash-separated); callers persist
// them with WriteTree, which also determines the eventual install root.
func Generate(c *Component, vendors []string) ([]GeneratedFile, error) {
	var files []GeneratedFile
	for _, name := range vendors {
		v, ok := VendorByName(name)
		if !ok {
			return nil, fmt.Errorf("aihost: unknown vendor %q", name)
		}

		tree, err := v.Plugin(c)
		if err != nil {
			return nil, fmt.Errorf("aihost: vendor %s: plugin: %w", name, err)
		}
		paths := make([]string, 0, len(tree))
		for p := range tree {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			files = append(files, GeneratedFile{Path: name + "/assets/" + p, Content: tree[p]})
		}

		wrapper, err := moduleWrapperFiles(c, name)
		if err != nil {
			return nil, fmt.Errorf("aihost: vendor %s: module wrapper: %w", name, err)
		}
		files = append(files, wrapper...)
	}
	return files, nil
}

// WriteTree writes each GeneratedFile under base (slash paths converted to
// the host separator), creating parent directories as needed, and returns the
// number of files written. Each file is written atomically: a ".tmp" sibling
// is written first, then renamed over the destination.
func WriteTree(files []GeneratedFile, base string) (int, error) {
	n := 0
	for _, f := range files {
		dst := filepath.Join(base, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return n, fmt.Errorf("aihost: mkdir %s: %w", filepath.Dir(dst), err)
		}
		tmp := dst + ".tmp"
		if err := os.WriteFile(tmp, f.Content, 0o644); err != nil {
			return n, fmt.Errorf("aihost: write %s: %w", tmp, err)
		}
		if err := os.Rename(tmp, dst); err != nil {
			return n, fmt.Errorf("aihost: rename %s -> %s: %w", tmp, dst, err)
		}
		n++
	}
	return n, nil
}

// renderPlain executes a non-Go template (go.mod, README.md) verbatim — no
// go/format pass, since the output isn't Go source.
func renderPlain(t *template.Template, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("aihost: template %s: %w", t.Name(), err)
	}
	return buf.Bytes(), nil
}

// goStringSliceLiteral renders ss as a Go []string composite literal (nil
// when empty, so generated agents with no tool list read cleanly).
func goStringSliceLiteral(ss []string) string {
	if len(ss) == 0 {
		return "nil"
	}
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = fmt.Sprintf("%q", s)
	}
	return "[]string{" + strings.Join(parts, ", ") + "}"
}

// agentLiteral holds pre-escaped Go literal fragments (already run through
// %q / goStringSliceLiteral) for one corral.Agent{} composite literal, so the
// template itself only interpolates — it never has to reason about escaping.
type agentLiteral struct {
	Name, Description, System, Model, Tools string
}

type goModData struct {
	Module, Vendor, Version, GoDirective string
}

type agentsGenData struct {
	Agents []agentLiteral
}

type componentGenData struct {
	Vendor string
}

type docGenData struct {
	Vendor string
}

type readmeData struct {
	Component, Vendor string
}

// moduleWrapperFiles renders the shared Go-module wrapper (go.mod +
// agents.gen.go + component.gen.go + doc.go + README.md) for one vendor.
func moduleWrapperFiles(c *Component, vendor string) ([]GeneratedFile, error) {
	var out []GeneratedFile

	modBuf, err := renderPlain(goModTpl, goModData{Module: c.Module, Vendor: vendor, Version: corralModuleVersion, GoDirective: generatedGoDirective})
	if err != nil {
		return nil, err
	}
	out = append(out, GeneratedFile{Path: vendor + "/go.mod", Content: modBuf})

	var agents []agentLiteral
	for _, a := range c.Assets {
		if a.Kind != KindAgent && a.Kind != KindSubagent {
			continue
		}
		agents = append(agents, agentLiteral{
			Name:        fmt.Sprintf("%q", a.Name),
			Description: fmt.Sprintf("%q", a.Description),
			System:      fmt.Sprintf("%q", a.PromptBody()),
			Model:       fmt.Sprintf("%q", a.Model),
			Tools:       goStringSliceLiteral(a.Tools),
		})
	}
	agentsBuf, err := renderFormatted(agentsGenTpl, agentsGenData{Agents: agents})
	if err != nil {
		return nil, err
	}
	out = append(out, GeneratedFile{Path: vendor + "/agents.gen.go", Content: agentsBuf})

	compBuf, err := renderFormatted(componentGenTpl, componentGenData{Vendor: vendor})
	if err != nil {
		return nil, err
	}
	out = append(out, GeneratedFile{Path: vendor + "/component.gen.go", Content: compBuf})

	docBuf, err := renderFormatted(docGenTpl, docGenData{Vendor: vendor})
	if err != nil {
		return nil, err
	}
	out = append(out, GeneratedFile{Path: vendor + "/doc.go", Content: docBuf})

	readmeBuf, err := renderPlain(readmeTpl, readmeData{Component: c.Name, Vendor: vendor})
	if err != nil {
		return nil, err
	}
	out = append(out, GeneratedFile{Path: vendor + "/README.md", Content: readmeBuf})

	return out, nil
}

var goModTpl = template.Must(template.New("go.mod").Parse(
	`module {{.Module}}/{{.Vendor}}

go {{.GoDirective}}

require github.com/inovacc/corral {{.Version}}
`))

var agentsGenTpl = template.Must(template.New("agents.gen.go").Parse(
	`// Code generated by corral. DO NOT EDIT.

package ` + wrapperPackage + `
{{if .Agents}}
import "github.com/inovacc/corral"
{{range .Agents}}
func init() {
	corral.Register(func() corral.Agent {
		return corral.Agent{
			Name:        {{.Name}},
			Description: {{.Description}},
			System:      {{.System}},
			Tools:       {{.Tools}},
			Model:       {{.Model}},
		}
	})
}
{{end}}{{end}}`))

var componentGenTpl = template.Must(template.New("component.gen.go").Parse(
	`// Code generated by corral. DO NOT EDIT.

package ` + wrapperPackage + `

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/inovacc/corral"
)

//go:embed all:assets
var assetsFS embed.FS

// New returns a corral.Agency for the "{{.Vendor}}" component, running
// against the process's current working directory ("."). This component's
// plugin tree was generated for the {{.Vendor}} host, but corral itself
// decides which provider actually runs — pass whichever runtime provider
// corral has registered (e.g. "claude", "codex", "agy", "grok", "kimi"; note
// corral does not register a "gemini" provider yet, even though this
// component's assets were generated for the gemini host).
func New(provider string) (*corral.Agency, error) {
	return corral.NewAgency(provider, ".")
}

// Install writes the embedded plugin tree under target, returning the number
// of files written.
func Install(target string) (int, error) {
	n := 0
	err := fs.WalkDir(assetsFS, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel("assets", p)
		if err != nil {
			return err
		}
		data, err := assetsFS.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		tmp := dst + ".tmp"
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, dst); err != nil {
			return err
		}
		n++
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
`))

var docGenTpl = template.Must(template.New("doc.go").Parse(
	`// Code generated by corral. DO NOT EDIT.

// Package component is a generated corral component for the "{{.Vendor}}"
// vendor: it wraps the installable plugin tree (embedded under assets/) and
// registers the component's agents with corral.
package ` + wrapperPackage + `
`))

var readmeTpl = template.Must(template.New("README.md").Parse(
	`# {{.Component}} ({{.Vendor}})

This is a corral-generated component: a {{.Vendor}} plugin tree (assets/)
plus a Go module wrapper that registers its agents with corral and can
install the tree programmatically.

## Install the plugin tree

    component.Install("<target-dir>")

## Run the component's agents

    agency, err := component.New("claude") // or any provider corral registers
`))
