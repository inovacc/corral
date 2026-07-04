/*
Copyright (c) 2026 inovacc
*/

package aihost

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeClaudeVendor is a self-contained stand-in for the real
// aihost/vendors/claude package (which cannot be imported here without an
// import cycle: it imports aihost, and this file's package IS aihost). Its
// Plugin output mirrors the real vendor's shape closely enough to exercise
// Generate's path-joining and the module-wrapper codegen.
type fakeClaudeVendor struct{}

func (fakeClaudeVendor) Name() string { return "claude" }

func (fakeClaudeVendor) Plugin(*Component) (map[string][]byte, error) {
	return map[string][]byte{
		".mcp.json":            []byte("{}\n"),
		"agents/researcher.md": []byte("---\nname: researcher\n---\n\ndo research\n"),
	}, nil
}

func sampleComponent() *Component {
	return &Component{
		Name: "suite", Module: "github.com/me/suite",
		MCP: MCPSpec{Command: "corral", Args: []string{"mcp", "serve"}},
		Assets: []Asset{
			{Kind: KindCommand, Name: "review", Description: "rv", Body: "# /review"},
			{Kind: KindAgent, Name: "researcher", Description: "r", System: "do research",
				Tools: []string{"Read", "Grep"}, Model: "sonnet"},
			{Kind: KindSkill, Name: "enrich", Description: "en", Body: "# enrich"},
		},
	}
}

func TestGenerate_EmitsExpectedTree(t *testing.T) {
	RegisterVendor(func() Vendor { return fakeClaudeVendor{} })

	files, err := Generate(sampleComponent(), []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, f := range files {
		got[filepath.ToSlash(f.Path)] = true
	}

	want := []string{
		"claude/go.mod",
		"claude/agents.gen.go",
		"claude/component.gen.go",
		"claude/doc.go",
		"claude/README.md",
		"claude/assets/.mcp.json",
		"claude/assets/agents/researcher.md",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("Generate() missing %s; got %v", w, got)
		}
	}
}

// TestGenerate_ComponentGenEmbedsDotfiles guards against a regression to
// `//go:embed assets` (which silently drops dot-prefixed files such as
// .mcp.json and .claude-plugin/plugin.json — Go's embed excludes anything
// starting with "." unless the "all:" prefix is used).
func TestGenerate_ComponentGenEmbedsDotfiles(t *testing.T) {
	RegisterVendor(func() Vendor { return fakeClaudeVendor{} })

	files, err := Generate(sampleComponent(), []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if filepath.ToSlash(f.Path) == "claude/component.gen.go" {
			if !strings.Contains(string(f.Content), "//go:embed all:assets") {
				t.Errorf("component.gen.go must embed with all:assets to include dotfiles, got:\n%s", f.Content)
			}
			return
		}
	}
	t.Fatal("claude/component.gen.go not found in generated files")
}

// TestGenerate_GoModUsesGeneratedGoDirective guards against the generated
// go.mod hardcoding a Go version independent of corral's own go.mod `go`
// directive — the generated module requires corral, so it must declare at
// least the Go version corral was built with.
func TestGenerate_GoModUsesGeneratedGoDirective(t *testing.T) {
	RegisterVendor(func() Vendor { return fakeClaudeVendor{} })

	files, err := Generate(sampleComponent(), []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if filepath.ToSlash(f.Path) == "claude/go.mod" {
			content := string(f.Content)
			if strings.Contains(content, "go 1.25") {
				t.Errorf("go.mod must not hardcode go 1.25, got:\n%s", content)
			}
			if !strings.Contains(content, "go "+generatedGoDirective) {
				t.Errorf("go.mod must contain the templated go directive %q, got:\n%s", generatedGoDirective, content)
			}
			return
		}
	}
	t.Fatal("claude/go.mod not found in generated files")
}

func TestGenerate_UnknownVendor(t *testing.T) {
	if _, err := Generate(sampleComponent(), []string{"does-not-exist"}); err == nil {
		t.Fatal("expected error for unknown vendor")
	}
}

// TestGenerate_GeneratedGoFilesParseCleanly is the network-free compile
// guarantee: every emitted *.gen.go / doc.go must be syntactically valid Go,
// proving renderFormatted's go/format pass actually produces well-formed
// source. (A full `go build` of the generated module needs network to fetch
// the corral require; parsing is the guard we can run offline.)
func TestGenerate_GeneratedGoFilesParseCleanly(t *testing.T) {
	RegisterVendor(func() Vendor { return fakeClaudeVendor{} })

	files, err := Generate(sampleComponent(), []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	parsed := 0
	for _, f := range files {
		if filepath.Ext(f.Path) != ".go" {
			continue
		}
		if _, err := parser.ParseFile(fset, f.Path, f.Content, 0); err != nil {
			t.Errorf("generated %s does not parse: %v\n%s", f.Path, err, f.Content)
			continue
		}
		parsed++
	}
	if parsed == 0 {
		t.Fatal("no .go files were generated to parse")
	}
}

func TestWriteTree(t *testing.T) {
	RegisterVendor(func() Vendor { return fakeClaudeVendor{} })

	files, err := Generate(sampleComponent(), []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	n, err := WriteTree(files, dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(files) {
		t.Fatalf("WriteTree wrote %d files, want %d", n, len(files))
	}
	if _, err := os.Stat(filepath.Join(dir, "claude", "go.mod")); err != nil {
		t.Fatalf("go.mod not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "claude", "assets", "agents", "researcher.md")); err != nil {
		t.Fatalf("assets/agents/researcher.md not written: %v", err)
	}
}
