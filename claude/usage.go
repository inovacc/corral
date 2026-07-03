package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/inovacc/corral"
)

// usageBaseURL is the Anthropic API base. Overridable in tests; in production it
// is the same host the Claude Code CLI calls.
var usageBaseURL = "https://api.anthropic.com"

const (
	// oauthBeta is the beta gate Claude Code sends on the /api/oauth/* routes.
	oauthBeta = "oauth-2025-04-20"
	// userAgent mirrors the CLI's claude-code/<version> UA. Kept as a pinned
	// value; the server does not appear to validate the exact version.
	userAgent = "claude-code/2.0.30"
)

// oauthCreds is the subscription OAuth credential Claude Code persists. Shape
// recovered from the CLI bundle (claudeAiOauth object).
type oauthCreds struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"` // epoch MILLISECONDS
	Scopes           []string `json:"scopes"`
	SubscriptionType *string  `json:"subscriptionType"`
}

// credsFile is the on-disk wrapper: {"claudeAiOauth": {...}}.
type credsFile struct {
	ClaudeAiOauth oauthCreds `json:"claudeAiOauth"`
}

// configDir resolves Claude Code's config dir: $CLAUDE_CONFIG_DIR or ~/.claude,
// exactly as the CLI does.
func configDir() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// loadCreds reads the subscription OAuth creds the same way Claude Code does:
// the plaintext ~/.claude/.credentials.json, falling back on macOS to the
// login keychain item (service "Claude Code-credentials").
func loadCreds() (oauthCreds, error) {
	dir, err := configDir()
	if err != nil {
		return oauthCreds{}, err
	}
	b, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	if err != nil {
		if runtime.GOOS == "darwin" {
			if kb, kerr := keychainCreds(); kerr == nil {
				b = kb
				err = nil
			}
		}
		if err != nil {
			return oauthCreds{}, fmt.Errorf("claude creds: %w", err)
		}
	}
	var cf credsFile
	if err := json.Unmarshal(b, &cf); err != nil {
		return oauthCreds{}, fmt.Errorf("claude creds parse: %w", err)
	}
	if cf.ClaudeAiOauth.AccessToken == "" {
		return oauthCreds{}, fmt.Errorf("claude creds: no OAuth access token (not logged in via subscription)")
	}
	return cf.ClaudeAiOauth, nil
}

// keychainCreds reads the credential JSON from the macOS login keychain, the
// CLI's storage backend on darwin.
func keychainCreds() ([]byte, error) {
	user := os.Getenv("USER")
	out, err := exec.Command("/usr/bin/security",
		"find-generic-password", "-a", user, "-w", "-s", "Claude Code-credentials").Output()
	if err != nil {
		return nil, err
	}
	return []byte(strings.TrimSpace(string(out))), nil
}

// apiWindow is a legacy top-level window in the /api/oauth/usage response.
type apiWindow struct {
	Utilization *float64 `json:"utilization"` // percent used, 0..100
	ResetsAt    *string  `json:"resets_at"`   // ISO-8601
}

// apiLimit is one entry in the response's generic limits[] array — the
// forward-compatible source of truth. Unlike the legacy top-level fields it also
// carries per-model ("scoped") weekly windows (e.g. Fable/Opus) and any windows
// Anthropic adds later.
type apiLimit struct {
	Group    string   `json:"group"`   // "session" | "weekly" | ...
	Kind     string   `json:"kind"`    // "session" | "weekly_all" | "weekly_scoped" | ...
	Percent  *float64 `json:"percent"` // 0..100
	ResetsAt *string  `json:"resets_at"`
	Severity string   `json:"severity"` // "normal" | "critical" | ...
	Scope    *struct {
		Model *struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

// apiUsage is the /api/oauth/usage response body.
type apiUsage struct {
	Limits       []apiLimit `json:"limits"` // preferred: generic per-window array
	FiveHour     *apiWindow `json:"five_hour"`
	SevenDay     *apiWindow `json:"seven_day"`
	SevenDayOpus *apiWindow `json:"seven_day_opus"`
}

// limitName gives a stable, human window name for a limits[] entry, folding the
// per-model scope into the label (e.g. weekly_scoped + "Fable" -> "weekly-Fable").
func limitName(l apiLimit) string {
	switch l.Kind {
	case "session":
		return "5h"
	case "weekly_all":
		return "weekly"
	case "weekly_scoped":
		if l.Scope != nil && l.Scope.Model != nil && l.Scope.Model.DisplayName != "" {
			return "weekly-" + l.Scope.Model.DisplayName
		}
		return "weekly-scoped"
	}
	switch {
	case l.Group != "" && l.Kind != "" && l.Group != l.Kind:
		return l.Group + "-" + l.Kind
	case l.Kind != "":
		return l.Kind
	default:
		return l.Group
	}
}

// ReadUsage performs the real Claude Code subscription-usage call:
// GET /api/oauth/usage with the OAuth bearer from the CLI's own credentials,
// exactly as the `/usage` and `/status` slash commands do. Returns the
// vendor-neutral corral.LimitStatus, or nil when not logged in via subscription.
func ReadUsage(ctx context.Context) (*corral.LimitStatus, error) {
	c, err := loadCreds()
	if err != nil {
		return nil, err
	}
	// Do NOT refresh here: the refresh flow rotates the one-time refresh_token
	// and persists it; doing that out-of-band would corrupt the CLI's stored
	// login. If the token is expired, surface it and let the agent refresh.
	if c.ExpiresAt > 0 && time.Now().UnixMilli() >= c.ExpiresAt {
		return nil, fmt.Errorf("claude oauth token expired (run `claude` to refresh)")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageBaseURL+"/api/oauth/usage", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("anthropic-beta", oauthBeta)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("claude usage: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("claude usage: HTTP %d", resp.StatusCode)
	}
	var u apiUsage
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("claude usage decode: %w", err)
	}

	s := &corral.LimitStatus{Source: "api/oauth/usage"}
	if c.SubscriptionType != nil {
		s.Plan = *c.SubscriptionType
	}
	parseReset := func(iso *string) time.Time {
		if iso != nil {
			if t, perr := time.Parse(time.RFC3339, *iso); perr == nil {
				return t
			}
		}
		return time.Time{}
	}
	// Prefer the generic limits[] array: it includes the per-model ("scoped")
	// weekly windows (e.g. Fable) that the legacy top-level fields omit.
	for _, l := range u.Limits {
		if l.Percent == nil {
			continue
		}
		s.Windows = append(s.Windows, corral.LimitWindow{
			Name:        limitName(l),
			UsedPercent: *l.Percent,
			ResetsAt:    parseReset(l.ResetsAt),
		})
	}
	if len(s.Windows) == 0 {
		// Fallback for older API shapes without limits[].
		add := func(name string, w *apiWindow) {
			if w == nil || w.Utilization == nil {
				return
			}
			s.Windows = append(s.Windows, corral.LimitWindow{
				Name: name, UsedPercent: *w.Utilization, ResetsAt: parseReset(w.ResetsAt),
			})
		}
		add("5h", u.FiveHour)
		add("weekly", u.SevenDay)
		add("weekly-opus", u.SevenDayOpus)
	}
	if len(s.Windows) == 0 {
		return nil, nil
	}
	return s, nil
}
