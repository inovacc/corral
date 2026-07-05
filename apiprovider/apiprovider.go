// Package apiprovider provides metered-API corral.Provider backends for
// OpenAI-compatible Chat Completions and Anthropic Messages, hand-rolled on the
// standard library (no third-party deps). Each backend is a plain corral.Provider
// plus an optional corral.UsageReporter, so it drops into the Agency, SessionPool,
// and monitor unchanged. OpenRouter has its own package (github.com/inovacc/corral/openrouter).
package apiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/inovacc/corral"
)

// Config selects and configures a hand-rolled API backend. Key is the resolved
// secret (the caller reads os.Getenv(<name>) — this package never sees the env
// name, only the value, and never logs it).
type Config struct {
	Format  string // "openai" | "anthropic"
	Model   string
	BaseURL string // optional; defaults per format
	Key     string // resolved secret
}

// New returns the hand-rolled backend for cfg.Format.
func New(cfg Config) (corral.Provider, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("apiprovider: model is required")
	}
	if cfg.Key == "" {
		return nil, fmt.Errorf("apiprovider: empty API key (is the key_env variable set?)")
	}
	switch cfg.Format {
	case "openai":
		return newOpenAI(cfg), nil
	case "anthropic":
		return newAnthropic(cfg), nil
	default:
		return nil, fmt.Errorf("apiprovider: unknown format %q (want openai|anthropic)", cfg.Format)
	}
}

// defaultClient bounds every backend call; httptest URLs are reachable with it.
func defaultClient() *http.Client { return &http.Client{Timeout: 120 * time.Second} }

// httpDo POSTs body as JSON to url with headers, honoring ctx, and returns the
// raw response body + headers. A non-2xx status is an error (body appended for
// diagnostics; the request key is a header, never echoed into the message).
func httpDo(ctx context.Context, hc *http.Client, url string, headers map[string]string, body any) ([]byte, http.Header, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("apiprovider: marshal request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, rdr)
	if err != nil {
		return nil, nil, fmt.Errorf("apiprovider: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("apiprovider: request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, fmt.Errorf("apiprovider: read body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return data, resp.Header, fmt.Errorf("apiprovider: status %d: %s", resp.StatusCode, truncate(data, 300))
	}
	return data, resp.Header, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// rateLimitStatus builds a one-window LimitStatus from a limit/remaining header
// pair, or nil when either is absent/unparseable (absence never blocks the fleet).
func rateLimitStatus(window string, h http.Header, limitHdr, remainingHdr string) *corral.LimitStatus {
	limit, lok := atoi(h.Get(limitHdr))
	remaining, rok := atoi(h.Get(remainingHdr))
	if !lok || !rok || limit <= 0 {
		return nil
	}
	used := float64(limit-remaining) / float64(limit) * 100
	return &corral.LimitStatus{Windows: []corral.LimitWindow{{Name: window, UsedPercent: used}}}
}

func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// placeholder backends fleshed out in openai.go / anthropic.go
func newOpenAI(cfg Config) corral.Provider { return &openaiProvider{cfg: cfg, hc: defaultClient()} }
func newAnthropic(cfg Config) corral.Provider {
	return &anthropicProvider{cfg: cfg, hc: defaultClient()}
}
