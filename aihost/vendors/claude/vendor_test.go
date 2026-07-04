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
