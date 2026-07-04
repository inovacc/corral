package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/inovacc/corral"
)

func TestLaunchSpecsComplete(t *testing.T) {
	specs := launchSpecs()
	for _, name := range []string{"claude", "codex", "agy", "grok", "kimi"} {
		s, ok := specs[name]
		if !ok {
			t.Fatalf("launchSpecs missing %q", name)
		}
		if s.bin == "" {
			t.Errorf("%s: empty bin", name)
		}
		if s.installURL == "" {
			t.Errorf("%s: empty installURL", name)
		}
	}
}

func TestFindBinary(t *testing.T) {
	// `go` is certainly on PATH while running `go test`.
	if _, ok := (launchSpec{bin: "go"}).findBinary(); !ok {
		t.Fatal("expected to find `go` on PATH")
	}

	// A bogus bin resolves via a real fallback file (Windows appends .exe).
	dir := t.TempDir()
	base := filepath.Join(dir, "mybin")
	real := base
	if runtime.GOOS == "windows" {
		real += ".exe"
	}
	if err := os.WriteFile(real, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := (launchSpec{bin: "no-such-bin-xyz", fallbacks: []string{base}}).findBinary(); !ok {
		t.Errorf("expected fallback resolution for %s", base)
	}

	// Missing everything.
	if _, ok := (launchSpec{bin: "no-such-bin-xyz", fallbacks: []string{filepath.Join(dir, "absent")}}).findBinary(); ok {
		t.Error("expected not-found for missing bin + fallback")
	}
}

func TestLaunchAliasResolvesToSpec(t *testing.T) {
	// The launch command resolves aliases via the provider registry; every
	// canonical provider must have a launch spec.
	for _, alias := range []string{"xai", "moonshot", "code", "antigravity"} {
		p, err := corral.ProviderByName(alias)
		if err != nil {
			t.Fatalf("alias %q not registered: %v", alias, err)
		}
		if _, ok := launchSpecs()[p.Name()]; !ok {
			t.Errorf("alias %q -> canonical %q has no launch spec", alias, p.Name())
		}
	}
}
