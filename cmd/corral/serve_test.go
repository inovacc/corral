package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/inovacc/corral"
)

// fakeUsage is a UsageReporter with a fixed snapshot for serve tests.
type fakeUsage struct{ used float64 }

func (f fakeUsage) Usage() (*corral.LimitStatus, error) {
	return &corral.LimitStatus{Plan: "test", Windows: []corral.LimitWindow{{Name: "5h", UsedPercent: f.used}}}, nil
}

func TestRunServe_OnceWritesBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	sink, err := corral.NewJSONLSink(path)
	if err != nil {
		t.Fatal(err)
	}
	watches := []serveWatch{{name: "fake", reporter: fakeUsage{used: 42}}}
	if err := runServe(context.Background(), watches, sink, serveOpts{interval: time.Minute, threshold: 80, once: true}); err != nil {
		t.Fatalf("runServe: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(ln) != "" {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("serve --once wrote %d lines, want 1 baseline\n%s", lines, b)
	}
	if !strings.Contains(string(b), `"provider":"fake"`) || !strings.Contains(string(b), `"worst_pct":42`) {
		t.Fatalf("baseline line missing expected fields: %s", b)
	}
}
