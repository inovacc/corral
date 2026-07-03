package grok

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestMapUsageWithAllowance feeds a sample billing body (with an included-credit
// allowance) through the decode/map function and asserts the derived percent
// window — no network. 250 of 1000 included credits used -> 25%.
func TestMapUsageWithAllowance(t *testing.T) {
	body := []byte(`{
		"subscription_tier":"supergrok",
		"includedUsed":250,
		"includedAllowance":1000,
		"totalUsed":250,
		"on_demand_enabled":false,
		"billingCycle":{"periodEnd":"2026-08-01T00:00:00Z"}
	}`)

	s, err := mapUsage(body)
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("nil status")
	}
	if s.Plan != "supergrok" {
		t.Errorf("plan = %q, want supergrok", s.Plan)
	}
	if s.Source != "grok billing" {
		t.Errorf("source = %q, want %q", s.Source, "grok billing")
	}
	if len(s.Windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(s.Windows))
	}
	w := s.Windows[0]
	if w.Name != "included" {
		t.Errorf("window name = %q, want included", w.Name)
	}
	if w.UsedPercent != 25 {
		t.Errorf("used = %v, want 25", w.UsedPercent)
	}
	if w.ResetsAt.IsZero() {
		t.Error("resetsAt should be parsed from billingCycle.periodEnd")
	}
	if s.Worst() != 25 {
		t.Errorf("worst = %v, want 25", s.Worst())
	}
}

// TestMapUsageClamp ensures a used>allowance overrun clamps to 100 rather than
// exceeding it.
func TestMapUsageClamp(t *testing.T) {
	body := []byte(`{"subscription_tier":"basic","includedUsed":1500,"includedTotal":1000}`)
	s, err := mapUsage(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Windows) != 1 || s.Windows[0].UsedPercent != 100 {
		t.Fatalf("want single 100%% window, got %+v", s.Windows)
	}
}

// TestMapUsageNoAllowance covers the documented limitation: with no allowance
// field, Grok gives raw counters but no denominator, so we surface usage with a
// single 0% "credits" window (percent unknown) and never fabricate a limit.
func TestMapUsageNoAllowance(t *testing.T) {
	body := []byte(`{"subscription_tier":"free","includedUsed":42,"totalUsed":42,"on_demand_enabled":true}`)
	s, err := mapUsage(body)
	if err != nil {
		t.Fatal(err)
	}
	if s.Plan != "free" {
		t.Errorf("plan = %q, want free", s.Plan)
	}
	if len(s.Windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(s.Windows))
	}
	if s.Windows[0].Name != "credits" {
		t.Errorf("window name = %q, want credits", s.Windows[0].Name)
	}
	if s.Windows[0].UsedPercent != 0 {
		t.Errorf("used = %v, want 0 (percent unknown)", s.Windows[0].UsedPercent)
	}
}

// TestMapUsageTolerant proves decode is tolerant: an unknown/empty payload never
// panics and still yields a usable (percent-unknown) status.
func TestMapUsageTolerant(t *testing.T) {
	for _, body := range []string{`{}`, `{"unknown_field":123}`, `{"subscriptionTier":"x"}`} {
		s, err := mapUsage([]byte(body))
		if err != nil {
			t.Fatalf("body %q: unexpected err %v", body, err)
		}
		if s == nil || len(s.Windows) != 1 {
			t.Fatalf("body %q: want a single-window status, got %+v", body, s)
		}
	}
}

// TestFetchUsageNotLoggedIn asserts the graceful path: no cred file and no BYOK
// key -> (nil, nil), returning before any HTTP so the fleet is never blocked and
// the test never hits the network.
func TestFetchUsageNotLoggedIn(t *testing.T) {
	t.Setenv("GROK_DIR", t.TempDir()) // empty dir: no auth.json
	t.Setenv("XAI_API_KEY", "")       // no BYOK fallback

	s, err := FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("want nil err, got %v", err)
	}
	if s != nil {
		t.Fatalf("want nil status when not logged in, got %+v", s)
	}
}

// TestLoadAuthSources checks the credential resolution order: file token wins;
// otherwise the XAI_API_KEY BYOK env; otherwise not logged in.
func TestLoadAuthSources(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GROK_DIR", dir)
	t.Setenv("XAI_API_KEY", "")

	// No file, no key -> not logged in.
	if _, ok := loadAuth(); ok {
		t.Fatal("want ok=false with no creds")
	}

	// BYOK key present -> used as bearer.
	t.Setenv("XAI_API_KEY", "xai-byok")
	a, ok := loadAuth()
	if !ok || a.AccessToken != "xai-byok" {
		t.Fatalf("BYOK: ok=%v token=%q", ok, a.AccessToken)
	}

	// File token wins over BYOK, and user_id is read.
	if err := os.WriteFile(filepath.Join(dir, "auth.json"),
		[]byte(`{"access_token":"sess-tok","user_id":"u-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a, ok = loadAuth()
	if !ok || a.AccessToken != "sess-tok" || a.UserID != "u-1" {
		t.Fatalf("file token: ok=%v token=%q user=%q", ok, a.AccessToken, a.UserID)
	}
}
