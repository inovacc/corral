package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inovacc/corral"
)

// Register a demo agent so the shared-file / install tests have a caller-supplied
// roster to render (the runtime ships no built-in roster).
func init() {
	corral.Register(func() corral.Agent {
		return corral.Agent{
			Name:        "demo",
			Kind:        corral.KindControl,
			Description: "example agent",
			Tools:       []string{"agents"},
			System:      "You are a demo agent.",
		}
	})
}

func TestRegistry(t *testing.T) {
	hosts := All()
	if len(hosts) != 3 {
		t.Fatalf("want 3 hosts, got %d", len(hosts))
	}
	for _, name := range []string{"claude", "codex", "agy"} {
		if _, ok := ByName(name); !ok {
			t.Errorf("host %q not registered", name)
		}
	}
}

func TestMCPManifest(t *testing.T) {
	m := string(MCPManifest("C:/bin/corral.exe"))
	for _, want := range []string{`"corral"`, `"mcp", "serve"`, `C:/bin/corral.exe`} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest missing %q:\n%s", want, m)
		}
	}
}

func TestSharedFilesHaveAgentsAndManifest(t *testing.T) {
	h, _ := ByName("codex")
	files, err := h.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files[".mcp.json"]; !ok {
		t.Error("missing .mcp.json")
	}
	if _, ok := files["agents/demo.md"]; !ok {
		t.Fatal("missing agents/demo.md")
	}
	// Agent md carries YAML frontmatter followed by the system prompt.
	db := string(files["agents/demo.md"])
	if !strings.HasPrefix(db, "---\nname: demo\n") {
		t.Errorf("demo.md frontmatter wrong:\n%s", db[:min(80, len(db))])
	}
	if !strings.Contains(db, "You are a demo agent.") {
		t.Error("demo.md missing system prompt")
	}
}

func TestInstallWritesTree(t *testing.T) {
	base := t.TempDir()
	h, _ := ByName("claude")

	// Dry-run writes nothing but plans files.
	dr, err := Install(h, base, true)
	if err != nil {
		t.Fatal(err)
	}
	if dr.Written != 0 || len(dr.Planned) == 0 {
		t.Fatalf("dry-run wrote=%d planned=%d", dr.Written, len(dr.Planned))
	}
	if _, err := os.Stat(filepath.Join(base, ".claude", installDir)); !os.IsNotExist(err) {
		t.Error("dry-run created files")
	}

	// Real install writes the tree under base/.claude/<installDir>.
	res, err := Install(h, base, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Written == 0 {
		t.Fatal("install wrote nothing")
	}
	for _, rel := range []string{".mcp.json", "README.md", "agents/demo.md"} {
		p := filepath.Join(base, ".claude", installDir, filepath.FromSlash(rel))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s: %v", rel, err)
		}
	}
}

func TestDoctor(t *testing.T) {
	base := t.TempDir()
	r := claudeHost{}.Doctor(base)
	if r.Host != "claude" || r.Verdict == "" || len(r.Checks) == 0 {
		t.Fatalf("bad report: %+v", r)
	}
}
