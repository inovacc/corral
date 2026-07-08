package apiprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/inovacc/corral"
)

type anthropicProvider struct {
	cfg     Config
	hc      *http.Client
	mu      sync.Mutex
	lastLim *corral.LimitStatus
}

func (p *anthropicProvider) Name() string { return "anthropic" }

func (p *anthropicProvider) baseURL() string {
	if p.cfg.BaseURL != "" {
		return strings.TrimRight(p.cfg.BaseURL, "/")
	}
	return "https://api.anthropic.com"
}

func (p *anthropicProvider) Run(ctx context.Context, req corral.RunRequest) (corral.RunResult, error) {
	schema := req.EffectiveSchema()
	native := schema != "" // anthropic enforces schema via a forced tool, not a prompt hint
	prompt := req.ComposePrompt(!native)

	body := map[string]any{
		"model":      p.cfg.Model,
		"max_tokens": 4096,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	}
	if native {
		body["tools"] = []map[string]any{{
			"name":         "result",
			"description":  "Return the structured result.",
			"input_schema": json.RawMessage(schema),
		}}
		body["tool_choice"] = map[string]any{"type": "tool", "name": "result"}
	}

	data, hdr, err := httpDo(ctx, p.hc, p.baseURL()+"/v1/messages", map[string]string{
		"x-api-key":         p.cfg.Key,
		"anthropic-version": "2023-06-01",
	}, body)
	p.record(hdr)
	if err != nil {
		return corral.RunResult{}, fmt.Errorf("anthropic: %w", err)
	}

	var out struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return corral.RunResult{}, fmt.Errorf("anthropic: decode: %w", err)
	}
	for _, c := range out.Content {
		if native && c.Type == "tool_use" {
			return corral.RunResult{Text: string(c.Input), Provider: p.Name()}, nil
		}
		if !native && c.Type == "text" {
			return corral.RunResult{Text: c.Text, Provider: p.Name()}, nil
		}
	}
	return corral.RunResult{}, fmt.Errorf("anthropic: no usable content block")
}

func (p *anthropicProvider) record(h http.Header) {
	s := rateLimitStatus("requests", h, "anthropic-ratelimit-requests-limit", "anthropic-ratelimit-requests-remaining")
	if s == nil {
		return
	}
	p.mu.Lock()
	p.lastLim = s
	p.mu.Unlock()
}

// Usage reports the rate-limit snapshot captured from the most recent Run's
// response headers, or (nil, nil) before the first Run — absence is never
// blocking (matching the CLI providers).
func (p *anthropicProvider) Usage(_ context.Context) (*corral.LimitStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastLim, nil
}
