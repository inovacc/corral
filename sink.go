package corral

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// UsageSink persists usage snapshots and alerts. It is the pluggable output seam
// for `corral serve`; JSONLSink is the default (append-only JSONL) implementation.
type UsageSink interface {
	WriteSample(Sample) error
	WriteAlert(Alert) error
	Close() error
}

// jsonlWindow / jsonlRecord are the on-disk JSONL shapes (snake_case).
type jsonlWindow struct {
	Name     string  `json:"name"`
	UsedPct  float64 `json:"used_pct"`
	ResetsAt string  `json:"resets_at,omitempty"`
}

type jsonlRecord struct {
	TS        string        `json:"ts"`
	Event     string        `json:"event"` // "sample" | "alert"
	Provider  string        `json:"provider"`
	Plan      string        `json:"plan,omitempty"`
	Source    string        `json:"source,omitempty"`
	WorstPct  float64       `json:"worst_pct"`
	Threshold float64       `json:"threshold,omitempty"`
	Windows   []jsonlWindow `json:"windows,omitempty"`
	Err       string        `json:"err,omitempty"`
}

// JSONLSink appends one JSON line per CHANGED sample and one per alert. It keeps
// a per-provider signature and skips samples whose signature is unchanged since
// the last written line, so an idle fleet produces almost no output. Every method
// is mutex-guarded: `corral serve` drives it from a single poll goroutine (so the
// lock is defensive there), and the guard keeps any future concurrent caller safe.
type JSONLSink struct {
	mu      sync.Mutex
	f       io.WriteCloser
	enc     *json.Encoder
	lastSig map[string]string
}

// NewJSONLSink opens (creating parent dirs and the file) path for append.
func NewJSONLSink(path string) (*JSONLSink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir usage-log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open usage log: %w", err)
	}
	return newJSONLSink(f), nil
}

// newJSONLSink builds a JSONLSink around any io.WriteCloser, allowing tests to
// inject a writer that fails so the dedup-rollback path can be exercised.
func newJSONLSink(w io.WriteCloser) *JSONLSink {
	return &JSONLSink{f: w, enc: json.NewEncoder(w), lastSig: map[string]string{}}
}

// sampleSignature is a stable string capturing everything a "change" cares about:
// worst percent (0.1 granularity), each window (name/used/reset), and the error.
func sampleSignature(s Sample) string {
	var b strings.Builder
	fmt.Fprintf(&b, "w=%.1f|", s.Worst)
	if s.Status != nil {
		for _, win := range s.Status.Windows {
			fmt.Fprintf(&b, "%s=%.1f@%d;", win.Name, win.UsedPercent, win.ResetsAt.Unix())
		}
	}
	b.WriteString("e=" + s.Err)
	return b.String()
}

func recordFromSample(s Sample) jsonlRecord {
	r := jsonlRecord{
		TS:       s.At.UTC().Format(time.RFC3339),
		Event:    "sample",
		Provider: s.Provider,
		WorstPct: s.Worst,
		Err:      s.Err,
	}
	if s.Status != nil {
		r.Plan = s.Status.Plan
		r.Source = s.Status.Source
		for _, win := range s.Status.Windows {
			jw := jsonlWindow{Name: win.Name, UsedPct: win.UsedPercent}
			if !win.ResetsAt.IsZero() {
				jw.ResetsAt = win.ResetsAt.UTC().Format(time.RFC3339)
			}
			r.Windows = append(r.Windows, jw)
		}
	}
	return r
}

// WriteSample writes a line only when this provider's signature changed since the
// last written line (the first sample per provider is always a baseline write).
func (s *JSONLSink) WriteSample(smp Sample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sig := sampleSignature(smp)
	if prev, ok := s.lastSig[smp.Provider]; ok && prev == sig {
		return nil
	}
	// Encode first; only record the signature as written if the write succeeded,
	// so a transient write error doesn't poison dedup (the next identical sample
	// will retry rather than being silently skipped).
	if err := s.enc.Encode(recordFromSample(smp)); err != nil {
		// json.Encoder latches its first write error permanently (all later
		// Encode calls short-circuit and return the cached error), so rebuild
		// it here too -- otherwise a retry would fail even once the
		// underlying writer recovers.
		s.enc = json.NewEncoder(s.f)
		return err
	}
	s.lastSig[smp.Provider] = sig
	return nil
}

// WriteAlert writes an alert line (alerts are already debounced by the Monitor).
func (s *JSONLSink) WriteAlert(a Alert) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enc.Encode(jsonlRecord{
		TS:        a.At.UTC().Format(time.RFC3339),
		Event:     "alert",
		Provider:  a.Provider,
		WorstPct:  a.Worst,
		Threshold: a.Threshold,
	})
}

func (s *JSONLSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.f.Close()
}
