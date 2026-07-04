package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const genFixture = `{
  "component": {"name":"suite","module":"github.com/me/suite","description":"d",
    "mcp":{"command":"corral","args":["mcp","serve"]}},
  "vendors": ["claude","gemini","codex"],
  "assets": [
    {"kind":"agent","name":"researcher","description":"r","system":"do research","tools":["Read"]},
    {"kind":"command","name":"review","description":"rv","body":"# review"},
    {"kind":"skill","name":"enrich","description":"en","body":"# enrich"}
  ]
}`

func writeGenFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "corral.json")
	if err := os.WriteFile(cfg, []byte(genFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestGenCmd_DryRun_ListsAllVendors(t *testing.T) {
	cfg := writeGenFixture(t)
	out := t.TempDir()

	cmd := newGenCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--dry-run", "--config", cfg, "--out", out})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	for _, want := range []string{"claude/go.mod", "gemini/go.mod", "codex/go.mod"} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run output missing %q; got:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "claude")); err == nil {
		t.Error("--dry-run must not write files")
	}
}

func TestGenListCmd_PrintsAssetCounts(t *testing.T) {
	cfg := writeGenFixture(t)

	cmd := newGenCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"list", "--config", cfg})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	for _, want := range []string{"agent", "command", "skill", "claude", "gemini", "codex"} {
		if !strings.Contains(got, want) {
			t.Errorf("list output missing %q; got:\n%s", want, got)
		}
	}
}

func TestGenCmd_Write_ReportsCount(t *testing.T) {
	cfg := writeGenFixture(t)
	out := t.TempDir()

	cmd := newGenCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--config", cfg, "--out", out, "--vendor", "claude"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(out, "claude", "go.mod")); err != nil {
		t.Fatalf("expected claude/go.mod written: %v", err)
	}
	if !strings.Contains(buf.String(), "claude") {
		t.Errorf("write summary missing vendor name; got:\n%s", buf.String())
	}
}
