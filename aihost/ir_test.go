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
