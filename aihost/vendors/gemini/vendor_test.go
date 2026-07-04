package gemini

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

func TestGeminiPlugin_EmitsExtensionTree(t *testing.T) {
	tree, err := (Host{}).Plugin(sampleComponent())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"gemini-extension.json", "GEMINI.md", "skills/enrich/SKILL.md",
		"skills/suite-command-library/SKILL.md", "skills/suite-agent-library/SKILL.md",
	} {
		if _, ok := tree[want]; !ok {
			t.Errorf("gemini tree missing %s", want)
		}
	}
	// no hook surface: hook assets must not leak into the tree.
	if _, ok := tree["hooks/hooks.json"]; ok {
		t.Errorf("gemini tree must not emit hooks.json (no hook surface)")
	}

	// frontmatter-\n invariant on a rendered skill md asset.
	if md := string(tree["skills/enrich/SKILL.md"]); !strings.Contains(md, "\n---\n") {
		t.Errorf("skill md frontmatter malformed:\n%s", md)
	}

	// command library indexes the command asset.
	if lib := string(tree["skills/suite-command-library/SKILL.md"]); !strings.Contains(lib, "review") {
		t.Errorf("command library missing review entry:\n%s", lib)
	}
	// agent library indexes the agent asset.
	if lib := string(tree["skills/suite-agent-library/SKILL.md"]); !strings.Contains(lib, "researcher") {
		t.Errorf("agent library missing researcher entry:\n%s", lib)
	}

	// gemini-extension.json wires the MCP server.
	if ext := string(tree["gemini-extension.json"]); !strings.Contains(ext, `"corral"`) {
		t.Errorf("gemini-extension.json missing mcp command:\n%s", ext)
	}
}

func TestGeminiHost_InstallTarget(t *testing.T) {
	got, err := (Host{}).InstallTarget("/base")
	if err != nil {
		t.Fatal(err)
	}
	// filepath.Join uses OS separators; normalize for comparison. Mirrors the
	// claude vendor's convention: InstallTarget returns the shared extensions
	// dir under base — the caller namespaces the component's own subdir
	// beneath it.
	norm := strings.ReplaceAll(got, "\\", "/")
	if !strings.HasSuffix(norm, "/.gemini/extensions") {
		t.Errorf("InstallTarget = %q, want suffix .gemini/extensions", got)
	}
}
