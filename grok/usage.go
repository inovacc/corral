package grok

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/inovacc/corral"
)

// grokClientVersion is the x-grok-client-version the CLI stamps on cli-chat-proxy
// requests (measured on the shipped v0.2.82 PE; see docs/kb/grok.md).
const grokClientVersion = "0.2.82"

// billingURL resolves the full billing endpoint. GROK_BILLING_URL overrides the
// whole URL (tests + custom proxies); otherwise it is the documented
// cli-chat-proxy billing route the CLI's `/usage` command fetches.
func billingURL() string {
	if u := os.Getenv("GROK_BILLING_URL"); u != "" {
		return u
	}
	return "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
}

// grokAuth is the subset of ~/.grok/auth.json the billing call needs. Grok
// stores the OAuth session token here (`grok login`). Fields are optional; a
// missing file or token falls back to the XAI_API_KEY BYOK env.
type grokAuth struct {
	AccessToken string `json:"access_token"`
	UserID      string `json:"user_id"`
}

// grokDir resolves ~/.grok (the dir Grok stores config + OAuth creds in).
// GROK_DIR overrides it (parity with the sibling providers' dir env, and for
// tests).
func grokDir() (string, error) {
	if d := os.Getenv("GROK_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".grok"), nil
}

// loadAuth reads the Grok session token from ~/.grok/auth.json, falling back to
// the XAI_API_KEY BYOK env when the file is absent, unreadable, or carries no
// token. Decode is tolerant: a garbage file is ignored, not fatal. Returns
// ok=false when neither source yields a bearer (not logged in) — the caller then
// reports (nil, nil) so monitoring never blocks the fleet.
func loadAuth() (grokAuth, bool) {
	var a grokAuth
	if dir, err := grokDir(); err == nil {
		if b, rerr := os.ReadFile(filepath.Join(dir, "auth.json")); rerr == nil {
			_ = json.Unmarshal(b, &a) // tolerant: ignore parse errors, fall through to BYOK
		}
	}
	if a.AccessToken == "" {
		a.AccessToken = os.Getenv("XAI_API_KEY") // BYOK fallback
	}
	return a, a.AccessToken != ""
}

// billingResponse is the tolerant decode target for GET /billing?format=credits.
// Grok returns raw credit counters with NO server-side percent, so every field
// is optional (json.Number zero-value "" == absent) and missing fields simply
// skip a window — decode never panics. Field names recovered from the v0.2.82 PE
// (docs/kb/grok.md).
type billingResponse struct {
	SubscriptionTier    string `json:"subscription_tier"`
	SubscriptionTierAlt string `json:"subscriptionTier"` // camelCase variant seen in the bundle

	IncludedUsed json.Number `json:"includedUsed"` // of the plan's included allowance
	TotalUsed    json.Number `json:"totalUsed"`    // decoded for completeness; not mapped to a window

	OnDemandEnabled *bool `json:"on_demand_enabled"` // pay-as-you-go toggle (decoded, not mapped)

	// The included-credit allowance (the denominator for a percent) is not
	// confirmed by name in the KB — Grok ships raw counters, not the total. We
	// decode several candidate names tolerantly; the first present one wins. When
	// none is present the true percent is UNKNOWN (see mapUsage).
	IncludedAllowance json.Number `json:"includedAllowance"`
	IncludedTotal     json.Number `json:"includedTotal"`
	Included          json.Number `json:"included"`
	IncludedLimit     json.Number `json:"includedLimit"`

	// periodEnd (billing-cycle end) may arrive top-level or nested under
	// billingCycle — accept both.
	PeriodEnd    string `json:"periodEnd"`
	BillingCycle struct {
		PeriodEnd string `json:"periodEnd"`
	} `json:"billingCycle"`
}

// mapUsage decodes a /billing?format=credits body into the vendor-neutral
// LimitStatus. It is split out from the HTTP call so it can be unit-tested with a
// sample body (no network). Tolerant: any missing field is skipped and it never
// panics.
//
// Percent derivation: Grok returns NO server percent. If the response carries an
// included-credit allowance we report a real window UsedPercent =
// includedUsed/allowance*100 (clamped 0..100). If NO allowance field is present
// we cannot compute a true percent, so we still surface usage — a single
// "credits" window at 0% (usage visible, percent UNKNOWN) — rather than
// fabricating a limit. This 0% is a KNOWN LIMITATION, not a real reading; it will
// become a real percent once Grok's allowance field name is confirmed.
func mapUsage(body []byte) (*corral.LimitStatus, error) {
	var b billingResponse
	if err := json.Unmarshal(body, &b); err != nil {
		return nil, err
	}

	plan := b.SubscriptionTier
	if plan == "" {
		plan = b.SubscriptionTierAlt
	}
	s := &corral.LimitStatus{Plan: plan, Source: "grok billing"}

	resetsAt := parsePeriodEnd(b)
	used, uok := numFloat(b.IncludedUsed)
	allowance, aok := firstNum(b.IncludedAllowance, b.IncludedTotal, b.Included, b.IncludedLimit)

	if uok && aok && allowance > 0 {
		s.Windows = append(s.Windows, corral.LimitWindow{
			Name:        "included",
			UsedPercent: clampPct(used / allowance * 100),
			ResetsAt:    resetsAt,
		})
		return s, nil
	}

	// No allowance denominator → percent unknown; surface usage without a limit.
	s.Windows = append(s.Windows, corral.LimitWindow{
		Name:        "credits",
		UsedPercent: 0,
		ResetsAt:    resetsAt,
	})
	return s, nil
}

// FetchUsage performs the real Grok subscription-billing call — the same
// GET /billing?format=credits the CLI's `/usage` command makes — authenticating
// with the session token in ~/.grok/auth.json (or the XAI_API_KEY BYOK env). It
// returns the vendor-neutral corral.LimitStatus.
//
// GRACEFUL (mirrors claude/usage.go's non-blocking philosophy): not-logged-in, a
// missing/garbage credential file, or any HTTP or decode error yields (nil, nil)
// so the monitor never blocks the fleet on a billing hiccup.
func FetchUsage(ctx context.Context) (*corral.LimitStatus, error) {
	a, ok := loadAuth()
	if !ok {
		return nil, nil // not logged in / no BYOK key — non-blocking
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, billingURL(), nil)
	if err != nil {
		return nil, nil
	}
	req.Header.Set("Authorization", "Bearer "+a.AccessToken)
	req.Header.Set("X-XAI-Token-Auth", "xai-grok-cli")
	req.Header.Set("x-grok-client-version", grokClientVersion)
	if a.UserID != "" {
		req.Header.Set("x-userid", a.UserID)
	}

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil // offline / DNS / TLS — non-blocking
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, nil // 401 not-authed, 5xx upstream, etc. — non-blocking
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil
	}
	s, err := mapUsage(body)
	if err != nil {
		return nil, nil // undecodable body — non-blocking
	}
	return s, nil
}

// parsePeriodEnd extracts the billing-cycle reset time from the response,
// accepting periodEnd top-level or nested under billingCycle. Tolerant: an
// absent or unparseable value yields the zero time (unknown reset).
func parsePeriodEnd(b billingResponse) time.Time {
	raw := b.PeriodEnd
	if raw == "" {
		raw = b.BillingCycle.PeriodEnd
	}
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

// firstNum returns the first parseable value across the candidate json.Number
// fields (absent fields are the zero value "").
func firstNum(ns ...json.Number) (float64, bool) {
	for _, n := range ns {
		if f, ok := numFloat(n); ok {
			return f, true
		}
	}
	return 0, false
}

// numFloat parses a json.Number, reporting ok=false for an absent ("") or
// non-numeric value.
func numFloat(n json.Number) (float64, bool) {
	if n == "" {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil
}

// clampPct constrains a computed percent to 0..100.
func clampPct(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
