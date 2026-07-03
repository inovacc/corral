package corral

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeReporter is a controllable UsageReporter for Monitor tests.
type fakeReporter struct {
	used float64 // worst-window UsedPercent to report
	err  error
}

func (f *fakeReporter) Usage() (*LimitStatus, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &LimitStatus{Plan: "test", Windows: []LimitWindow{{Name: "5h", UsedPercent: f.used}}}, nil
}

func TestMonitorRecordsSamples(t *testing.T) {
	r := &fakeReporter{used: 42}
	m := NewMonitor(WithThreshold(80))
	m.Watch("acme", r)
	m.Poll(context.Background())

	if got := m.Latest()["acme"].Worst; got != 42 {
		t.Fatalf("latest worst = %v, want 42", got)
	}
	r.used = 55
	m.Poll(context.Background())
	if h := m.Samples("acme"); len(h) != 2 || h[1].Worst != 55 {
		t.Fatalf("history = %+v, want 2 samples ending at 55", h)
	}
}

func TestMonitorAlertsOncePerCrossing(t *testing.T) {
	r := &fakeReporter{used: 50}
	var alerts []Alert
	m := NewMonitor(WithThreshold(80), OnAlert(func(a Alert) { alerts = append(alerts, a) }))
	m.Watch("acme", r)

	m.Poll(context.Background()) // 50% — below
	if len(alerts) != 0 {
		t.Fatalf("no alert expected below threshold, got %d", len(alerts))
	}
	r.used = 85
	m.Poll(context.Background()) // 85% — crosses -> 1 alert
	m.Poll(context.Background()) // still 85% — debounced, no new alert
	if len(alerts) != 1 {
		t.Fatalf("want exactly 1 alert on crossing, got %d", len(alerts))
	}
	if alerts[0].Provider != "acme" || alerts[0].Worst != 85 || alerts[0].Threshold != 80 {
		t.Fatalf("alert payload wrong: %+v", alerts[0])
	}

	r.used = 40
	m.Poll(context.Background()) // recovers below
	r.used = 90
	m.Poll(context.Background()) // crosses again -> 2nd alert
	if len(alerts) != 2 {
		t.Fatalf("want re-alert after recovery, got %d", len(alerts))
	}
}

func TestMonitorErrorPollDoesNotAlert(t *testing.T) {
	r := &fakeReporter{err: errors.New("not logged in")}
	var alerts int
	m := NewMonitor(WithThreshold(80), OnAlert(func(Alert) { alerts++ }))
	m.Watch("acme", r)
	m.Poll(context.Background())

	s := m.Latest()["acme"]
	if s.Err == "" || s.Status != nil {
		t.Fatalf("errored poll should record Err and nil Status, got %+v", s)
	}
	if alerts != 0 {
		t.Fatalf("no alert on a failed poll, got %d", alerts)
	}
}

func TestMonitorHistoryCap(t *testing.T) {
	r := &fakeReporter{used: 10}
	m := NewMonitor(WithHistory(3), WithThreshold(0)) // alerting off
	m.Watch("acme", r)
	for i := 0; i < 5; i++ {
		m.Poll(context.Background())
	}
	if h := m.Samples("acme"); len(h) != 3 {
		t.Fatalf("history should cap at 3, got %d", len(h))
	}
}

func TestMonitorRunStopsOnContext(t *testing.T) {
	m := NewMonitor(WithInterval(10 * time.Millisecond))
	m.Watch("acme", &fakeReporter{used: 5})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()

	// Wait for the immediate first poll to land before cancelling (no race).
	deadline := time.After(2 * time.Second)
	for len(m.Samples("acme")) == 0 {
		select {
		case <-deadline:
			t.Fatal("Run should have polled at least once")
		case <-time.After(2 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
