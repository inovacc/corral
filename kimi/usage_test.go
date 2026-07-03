package kimi

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// sampleUsage is a representative /usages body exercising every documented
// shape: a top-level usage summary (used/limit), a limits[] entry with numbers
// nested under detail + a window{duration,timeUnit} label, a flat entry using
// remaining (with a relative reset_in), a clamp case (used > limit), and a junk
// row that must be skipped.
const sampleUsage = `{
  "usage": {"name":"Weekly limit","limit":1000,"used":850,"reset_at":"2026-08-01T00:00:00Z"},
  "limits": [
    {"detail":{"limit":100,"remaining":20},"window":{"duration":5,"timeUnit":"HOUR"}},
    {"name":"daily","limit":200,"used":50,"reset_in":3600},
    {"name":"overflow","limit":10,"used":999},
    {"name":"broken"}
  ]
}`

func TestDecodeUsageMapsWindows(t *testing.T) {
	s := decodeUsage([]byte(sampleUsage))
	if s == nil {
		t.Fatal("decodeUsage returned nil for a valid body")
	}
	if s.Plan != "kimi" || s.Source != "usages" {
		t.Errorf("plan/source = %q/%q, want kimi/usages", s.Plan, s.Source)
	}
	if len(s.Windows) != 4 { // "broken" (no limit) dropped
		t.Fatalf("windows = %d, want 4: %+v", len(s.Windows), s.Windows)
	}

	// Order is preserved: summary first, then limits[] in order.
	byName := map[string]time.Time{}
	pct := map[string]float64{}
	for _, w := range s.Windows {
		pct[w.Name] = w.UsedPercent
		byName[w.Name] = w.ResetsAt
	}

	if pct["Weekly limit"] != 85 { // 850/1000
		t.Errorf("weekly used = %v, want 85", pct["Weekly limit"])
	}
	if pct["5h limit"] != 80 { // (100-20)/100 via detail + window label
		t.Errorf("5h used = %v, want 80", pct["5h limit"])
	}
	if pct["daily"] != 25 { // 50/200
		t.Errorf("daily used = %v, want 25", pct["daily"])
	}
	if pct["overflow"] != 100 { // 999/10 clamped
		t.Errorf("overflow used = %v, want 100 (clamped)", pct["overflow"])
	}

	if byName["Weekly limit"].IsZero() {
		t.Error("weekly reset_at (RFC3339) not parsed")
	}
	if !byName["5h limit"].IsZero() {
		t.Errorf("5h reset should be zero (no field), got %v", byName["5h limit"])
	}
	if byName["daily"].IsZero() {
		t.Error("daily reset_in (relative seconds) not resolved")
	}

	if s.Worst() != 100 {
		t.Errorf("worst = %v, want 100", s.Worst())
	}
}

func TestDecodeUsageTolerant(t *testing.T) {
	for _, body := range []string{
		``,                        // empty
		`not json`,                // garbage
		`{}`,                      // no rows
		`{"limits":[]}`,           // empty limits
		`{"usage":{"name":"x"}}`,  // row without numbers -> skipped -> nil
		`{"limits":[{"used":5}]}`, // used but no limit -> skipped -> nil
	} {
		if got := decodeUsage([]byte(body)); got != nil {
			t.Errorf("decodeUsage(%q) = %+v, want nil", body, got)
		}
	}
}

func TestResolveTokenOAuthAndBYOK(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	t.Setenv("KIMI_API_KEY", "")

	// No credential store yet, no BYOK -> nothing resolvable.
	if _, ok := resolveToken(); ok {
		t.Fatal("resolveToken should be empty with no creds and no BYOK")
	}

	// BYOK fallback.
	t.Setenv("KIMI_API_KEY", "sk-byok")
	if tok, ok := resolveToken(); !ok || tok != "sk-byok" {
		t.Fatalf("BYOK fallback = %q,%v; want sk-byok,true", tok, ok)
	}

	// A live OAuth credential in the store wins over BYOK.
	credDir := filepath.Join(home, "credentials")
	if err := os.MkdirAll(credDir, 0o755); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour).UnixMilli()
	writeFile(t, filepath.Join(credDir, "oauth_kimi-code-env-abc.json"),
		`{"access_token":"tok-oauth","refresh_token":"r","expires_at":`+itoa(future)+`,"token_type":"Bearer"}`)
	if tok, ok := resolveToken(); !ok || tok != "tok-oauth" {
		t.Fatalf("OAuth token = %q,%v; want tok-oauth,true", tok, ok)
	}
}

func TestOAuthTokenSkipsExpired(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	t.Setenv("KIMI_API_KEY", "")
	credDir := filepath.Join(home, "credentials")
	if err := os.MkdirAll(credDir, 0o755); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour).UnixMilli()
	writeFile(t, filepath.Join(credDir, "stale.json"),
		`{"access_token":"tok-stale","expires_at":`+itoa(past)+`}`)
	if _, ok := oauthToken(); ok {
		t.Fatal("expired token must be skipped")
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// itoa renders an int64 without pulling strconv into the table above.
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
