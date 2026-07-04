package codex

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

func TestCodexPlugin_EmitsPluginTree(t *testing.T) {
	tree, err := (Host{}).Plugin(sampleComponent())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		".codex-plugin/plugin.json", ".mcp.json", "skills/enrich/SKILL.md",
		"skills/suite-command-library/SKILL.md", "skills/suite-agent-library/SKILL.md",
	} {
		if _, ok := tree[want]; !ok {
			t.Errorf("codex tree missing %s", want)
		}
	}
	// no hook surface: hook assets must not leak into the tree.
	if _, ok := tree["hooks/hooks.json"]; ok {
		t.Errorf("codex tree must not emit hooks.json (no hook surface)")
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

	// plugin.json wires the component metadata.
	if pj := string(tree[".codex-plugin/plugin.json"]); !strings.Contains(pj, `"suite"`) {
		t.Errorf("plugin.json missing component name:\n%s", pj)
	}

	// .mcp.json wires the MCP server.
	if mj := string(tree[".mcp.json"]); !strings.Contains(mj, `"corral"`) {
		t.Errorf(".mcp.json missing mcp command:\n%s", mj)
	}
}

func TestCodexHost_InstallTarget(t *testing.T) {
	got, err := (Host{}).InstallTarget("/base")
	if err != nil {
		t.Fatal(err)
	}
	// filepath.Join uses OS separators; normalize for comparison. Mirrors the
	// claude/gemini vendors' convention: InstallTarget returns the shared
	// plugins dir under base — the caller namespaces the component's own
	// subdir beneath it.
	norm := strings.ReplaceAll(got, "\\", "/")
	if !strings.HasSuffix(norm, "/.codex/plugins") {
		t.Errorf("InstallTarget = %q, want suffix .codex/plugins", got)
	}
}
