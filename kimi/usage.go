package kimi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/inovacc/corral"
)

// kimiBaseURL is the managed Kimi-for-Coding API base the CLI's `/usages` call
// targets. Default recovered from the shipped bundle (DEFAULT_KIMI_CODE_BASE_URL);
// overridable via KIMI_CODE_BASE_URL then KIMI_BASE_URL for parity with the CLI
// (and to point tests at a stub). Evaluated once at init.
var kimiBaseURL = func() string {
	if b := os.Getenv("KIMI_CODE_BASE_URL"); b != "" {
		return strings.TrimRight(b, "/")
	}
	if b := os.Getenv("KIMI_BASE_URL"); b != "" {
		return strings.TrimRight(b, "/")
	}
	return "https://api.kimi.com/coding/v1"
}()

// kimiCred is the tolerant subset of a Kimi Code credential entry. The managed
// OAuth token is persisted as a JSON file under ~/.kimi-code/credentials/ keyed
// oauth/kimi-code-env-<sha256>; we scan for any file carrying an access_token
// rather than hard-coding the sanitized filename. expires_at may be an RFC3339
// string, epoch seconds, or epoch millis (decoded as any, honored if present).
type kimiCred struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    any    `json:"expires_at"`
	TokenType    string `json:"token_type"`
}

// credentialsDir resolves ~/.kimi-code/credentials (KIMI_CODE_HOME overrides the
// config home, mainly for tests). This is the OAuth token store dir.
func credentialsDir() (string, error) {
	if d := os.Getenv("KIMI_CODE_HOME"); d != "" {
		return filepath.Join(d, "credentials"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kimi-code", "credentials"), nil
}

// oauthToken scans the credential store for the first JSON entry carrying a live
// access_token. Tolerant: it walks the dir, ignores anything that is not a JSON
// object with an access_token, and skips tokens whose expires_at is in the past.
// Returns ("", false) when nothing usable is found (never an error — absence is
// not a failure that should block the fleet).
func oauthToken() (string, bool) {
	dir, err := credentialsDir()
	if err != nil {
		return "", false
	}
	var token string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() || token != "" {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		var c kimiCred
		if json.Unmarshal(b, &c) != nil {
			return nil
		}
		if strings.TrimSpace(c.AccessToken) == "" || credExpired(c.ExpiresAt) {
			return nil
		}
		token = c.AccessToken
		return nil
	})
	if token == "" {
		return "", false
	}
	return token, true
}

// resolveToken returns the bearer for the usage call: the managed OAuth access
// token if logged in, else the BYOK Moonshot key (KIMI_API_KEY). The BYOK path
// 404s on /usages (managed-plan feature) — that degrades gracefully to nil.
func resolveToken() (string, bool) {
	if tok, ok := oauthToken(); ok {
		return tok, true
	}
	if k := strings.TrimSpace(os.Getenv("KIMI_API_KEY")); k != "" {
		return k, true
	}
	return "", false
}

// ReadUsage performs the real Kimi Code subscription-usage call:
// GET {base}/usages with the managed OAuth bearer read from the CLI's own
// ~/.kimi-code/credentials/ store, exactly as `kimi usage` / the `/usages`
// section do. Returns the vendor-neutral corral.LimitStatus.
//
// GRACEFUL by design: not-logged-in, a missing/unreadable credential store, any
// HTTP failure, a non-200, or an undecodable body all return (nil, nil) so the
// monitor NEVER blocks the fleet on absence of data — only a positive
// over-limit signal ever withholds work.
func ReadUsage(ctx context.Context) (*corral.LimitStatus, error) {
	token, ok := resolveToken()
	if !ok {
		return nil, nil // not logged in / no BYOK — never block the fleet
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kimiBaseURL+"/usages", nil)
	if err != nil {
		return nil, nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, nil // 401 (re-login) / 404 (BYOK, not managed) / 5xx — degrade
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil
	}
	return decodeUsage(body), nil
}

// decodeUsage parses the /usages body and maps it to the vendor-neutral status.
// It is the pure decode/map seam the unit test drives (no network). Tolerant:
// every field is optional; a row that yields no used-percent is skipped, and an
// undecodable body (or one with no usable rows) maps to nil.
func decodeUsage(body []byte) *corral.LimitStatus {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	s := &corral.LimitStatus{Plan: "kimi", Source: "usages"}
	// Top-level usage summary row (labeled "Weekly limit" by the CLI).
	if u, ok := raw["usage"].(map[string]any); ok {
		if w, ok := rowWindow(u, "Weekly limit"); ok {
			s.Windows = append(s.Windows, w)
		}
	}
	// Per-window limits[].
	if limits, ok := raw["limits"].([]any); ok {
		for _, item := range limits {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if w, ok := rowWindow(m, ""); ok {
				s.Windows = append(s.Windows, w)
			}
		}
	}
	if len(s.Windows) == 0 {
		return nil
	}
	return s
}

// rowWindow maps one usage/limits row to a LimitWindow, reporting whether it
// looked like one (i.e. yielded a used-percent). Numbers may sit on the row or
// nested under "detail"; the name is the row's name/title, else a window label.
func rowWindow(m map[string]any, defaultName string) (corral.LimitWindow, bool) {
	used, ok := rowUsedPercent(m)
	if !ok {
		return corral.LimitWindow{}, false
	}
	name := firstString(m, "name", "title")
	if name == "" {
		name = windowLabel(m)
	}
	if name == "" {
		name = defaultName
	}
	return corral.LimitWindow{Name: name, UsedPercent: used, ResetsAt: rowReset(m)}, true
}

// rowUsedPercent derives 0..100 from a row: used/limit, else (limit-remaining)/
// limit. Looks on the row and a nested "detail". Returns false when no limit or
// no used/remaining is present.
func rowUsedPercent(m map[string]any) (float64, bool) {
	for _, src := range []map[string]any{m, nestedMap(m, "detail")} {
		if src == nil {
			continue
		}
		limit, lok := toFloat(src["limit"])
		if !lok || limit <= 0 {
			continue
		}
		if used, ok := toFloat(src["used"]); ok {
			return clampPct(used / limit * 100), true
		}
		if rem, ok := toFloat(src["remaining"]); ok {
			return clampPct((limit - rem) / limit * 100), true
		}
	}
	return 0, false
}

// windowLabel renders a "5h limit" / "7d limit" style label from a nested
// window{duration,timeUnit} object, or "" when absent/unrecognized.
func windowLabel(m map[string]any) string {
	win := nestedMap(m, "window")
	if win == nil {
		return ""
	}
	dur, ok := toFloat(win["duration"])
	if !ok || dur <= 0 {
		return ""
	}
	switch unit, _ := win["timeUnit"].(string); strings.ToUpper(unit) {
	case "MINUTE":
		return fmt.Sprintf("%dm limit", int(dur))
	case "HOUR":
		return fmt.Sprintf("%dh limit", int(dur))
	case "DAY":
		return fmt.Sprintf("%dd limit", int(dur))
	}
	return ""
}

// rowReset resolves a window reset time: an absolute reset_at|resetAt|reset_time|
// resetTime (RFC3339 or epoch), else a relative reset_in|resetIn|ttl|window in
// seconds. Checks the row and a nested "detail". Zero time when unknown.
func rowReset(m map[string]any) time.Time {
	for _, src := range []map[string]any{m, nestedMap(m, "detail")} {
		if src == nil {
			continue
		}
		for _, k := range []string{"reset_at", "resetAt", "reset_time", "resetTime"} {
			if t, ok := parseAbsReset(src[k]); ok && !t.IsZero() {
				return t
			}
		}
		for _, k := range []string{"reset_in", "resetIn", "ttl", "window"} {
			if secs, ok := toFloat(src[k]); ok && secs > 0 {
				return time.Now().Add(time.Duration(secs) * time.Second)
			}
		}
	}
	return time.Time{}
}

// credExpired reports whether a credential's expires_at is in the past. Absent,
// unparseable, or a sentinel near-zero epoch => not expired (assume usable).
func credExpired(v any) bool {
	t, ok := parseAbsReset(v)
	if !ok || t.IsZero() || t.Year() < 2000 {
		return false
	}
	return time.Now().After(t)
}

// parseAbsReset parses an absolute instant from a string (RFC3339 or numeric
// epoch) or a number (epoch seconds/millis). Reports whether it parsed.
func parseAbsReset(v any) (time.Time, bool) {
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return time.Time{}, false
		}
		if ts, err := time.Parse(time.RFC3339, s); err == nil {
			return ts, true
		}
		if f, ok := toFloat(s); ok {
			return epochToTime(f), true
		}
	case float64:
		return epochToTime(t), true
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return epochToTime(f), true
		}
	}
	return time.Time{}, false
}

// epochToTime interprets a numeric epoch as milliseconds when it is too large to
// be seconds, else as seconds.
func epochToTime(f float64) time.Time {
	if f > 1e12 {
		return time.UnixMilli(int64(f))
	}
	return time.Unix(int64(f), 0)
}

// nestedMap returns m[key] as a map, or nil.
func nestedMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

// firstString returns the first non-blank string value among keys.
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// toFloat coerces a JSON scalar (float64, json.Number, or numeric string) to a
// float64, reporting whether it was numeric.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		var f float64
		_, err := fmt.Sscan(strings.TrimSpace(n), &f)
		return f, err == nil
	}
	return 0, false
}

// clampPct bounds a percent to 0..100.
func clampPct(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
