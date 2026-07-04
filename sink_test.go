package corral

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad json line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func sampleAt(provider string, used float64, ts time.Time) Sample {
	return Sample{
		Provider: provider,
		Worst:    used,
		Status:   &LimitStatus{Plan: "p", Source: "s", Windows: []LimitWindow{{Name: "5h", UsedPercent: used}}},
		At:       ts,
	}
}

func TestJSONLSink_DedupsUnchangedSamples(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	sink, err := NewJSONLSink(path)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Unix(1_700_000_000, 0).UTC()
	if err := sink.WriteSample(sampleAt("claude", 42, ts)); err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteSample(sampleAt("claude", 42, ts.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	_ = sink.Close()

	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("unchanged sample written %d times, want 1 (dedup)", len(lines))
	}
	if lines[0]["event"] != "sample" || lines[0]["provider"] != "claude" || lines[0]["worst_pct"].(float64) != 42 {
		t.Fatalf("unexpected record: %v", lines[0])
	}
}

func TestJSONLSink_WritesOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	sink, _ := NewJSONLSink(path)
	ts := time.Unix(1_700_000_000, 0).UTC()
	_ = sink.WriteSample(sampleAt("claude", 42, ts))
	_ = sink.WriteSample(sampleAt("claude", 55, ts)) // worst changed
	_ = sink.Close()
	if lines := readLines(t, path); len(lines) != 2 {
		t.Fatalf("change produced %d lines, want 2", len(lines))
	}
}

func TestJSONLSink_WritesAlert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	sink, _ := NewJSONLSink(path)
	_ = sink.WriteAlert(Alert{Provider: "claude", Worst: 81, Threshold: 80, At: time.Unix(1_700_000_000, 0).UTC()})
	_ = sink.Close()
	lines := readLines(t, path)
	if len(lines) != 1 || lines[0]["event"] != "alert" || lines[0]["threshold"].(float64) != 80 {
		t.Fatalf("alert record wrong: %v", lines)
	}
}

// failWriter fails Write after okWrites successful writes, to exercise the
// dedup-rollback path.
type failWriter struct {
	okWrites int
	n        int
}

func (w *failWriter) Write(p []byte) (int, error) {
	w.n++
	if w.n > w.okWrites {
		return 0, errors.New("disk full")
	}
	return len(p), nil
}
func (w *failWriter) Close() error { return nil }

func TestJSONLSink_WriteFailureDoesNotPoisonDedup(t *testing.T) {
	fw := &failWriter{okWrites: 0} // every write fails
	sink := newJSONLSink(fw)
	ts := time.Unix(1_700_000_000, 0).UTC()

	if err := sink.WriteSample(sampleAt("claude", 42, ts)); err == nil {
		t.Fatal("expected write error, got nil")
	}
	// The failed write must NOT have recorded the signature — a retry of the
	// same sample must attempt the write again (not be deduped away).
	fw.okWrites = 10 // now writes succeed
	if err := sink.WriteSample(sampleAt("claude", 42, ts)); err != nil {
		t.Fatalf("retry after failed write should succeed, got %v", err)
	}
	if fw.n != 2 {
		t.Fatalf("expected 2 write attempts (fail then retry), got %d — dedup was poisoned", fw.n)
	}
}

func TestJSONLSink_ErrSampleNoWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	sink, _ := NewJSONLSink(path)
	_ = sink.WriteSample(Sample{Provider: "grok", Err: "usage timeout", At: time.Unix(1_700_000_000, 0).UTC()})
	_ = sink.Close()
	lines := readLines(t, path)
	if len(lines) != 1 || lines[0]["err"] != "usage timeout" {
		t.Fatalf("err sample record wrong: %v", lines)
	}
	if _, hasWindows := lines[0]["windows"]; hasWindows {
		t.Fatalf("err sample should omit windows, got: %v", lines[0])
	}
}
