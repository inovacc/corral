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

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "corral.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_ExecutionAPIMode(t *testing.T) {
	c, err := Load(writeCfg(t, `{
	  "component": {"name":"s","module":"github.com/me/s"},
	  "vendors": ["claude","codex"],
	  "execution": {
	    "claude": {"mode":"api","config":{"format":"openrouter","model":"anthropic/claude-sonnet-4","key_env":"OPENROUTER_API_KEY"}},
	    "codex":  {"mode":"subscription"}
	  },
	  "assets": []
	}`))
	if err != nil {
		t.Fatal(err)
	}
	got := c.Executions["claude"]
	if got.Mode != "api" || got.Config.Format != "openrouter" || got.Config.KeyEnv != "OPENROUTER_API_KEY" {
		t.Fatalf("claude execution = %+v", got)
	}
	if c.Executions["codex"].Mode != "subscription" {
		t.Fatalf("codex execution = %+v", c.Executions["codex"])
	}
}

func TestLoad_AbsentExecutionIsSubscription(t *testing.T) {
	c, err := Load(writeCfg(t, `{
	  "component": {"name":"s","module":"github.com/me/s"},
	  "vendors": ["claude"], "assets": []
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Executions["claude"]; ok {
		t.Error("absent execution must not create an entry (defaults to subscription downstream)")
	}
}

func TestLoad_RejectsLiteralKeyInKeyEnv(t *testing.T) {
	_, err := Load(writeCfg(t, `{
	  "component": {"name":"s","module":"github.com/me/s"},
	  "vendors": ["claude"],
	  "execution": {"claude": {"mode":"api","config":{"format":"openai","model":"gpt-4o","key_env":"sk-abc-123"}}},
	  "assets": []
	}`))
	if err == nil {
		t.Fatal("want error: key_env must be an env-var NAME, not a literal key")
	}
}

func TestLoad_RejectsBadModeAndFormat(t *testing.T) {
	if _, err := Load(writeCfg(t, `{
	  "component":{"name":"s","module":"github.com/me/s"},"vendors":["claude"],
	  "execution":{"claude":{"mode":"metered"}},"assets":[]}`)); err == nil {
		t.Error("want error on unknown mode")
	}
	if _, err := Load(writeCfg(t, `{
	  "component":{"name":"s","module":"github.com/me/s"},"vendors":["claude"],
	  "execution":{"claude":{"mode":"api","config":{"format":"cohere","model":"m","key_env":"K"}}},"assets":[]}`)); err == nil {
		t.Error("want error on unknown format")
	}
	if _, err := Load(writeCfg(t, `{
	  "component":{"name":"s","module":"github.com/me/s"},"vendors":["claude"],
	  "execution":{"claude":{"mode":"api","config":{"format":"openai","model":"","key_env":"K"}}},"assets":[]}`)); err == nil {
		t.Error("want error on missing model in api mode")
	}
}
