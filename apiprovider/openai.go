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

type openaiProvider struct {
	cfg     Config
	hc      *http.Client
	mu      sync.Mutex
	lastLim *corral.LimitStatus
}

func (p *openaiProvider) Name() string { return "openai" }

func (p *openaiProvider) baseURL() string {
	if p.cfg.BaseURL != "" {
		return strings.TrimRight(p.cfg.BaseURL, "/")
	}
	return "https://api.openai.com/v1"
}

func (p *openaiProvider) Run(ctx context.Context, req corral.RunRequest) (corral.RunResult, error) {
	schema := req.EffectiveSchema()
	native := schema != "" // openai enforces schema via response_format, not a prompt hint
	prompt := req.ComposePrompt(!native)

	body := map[string]any{
		"model":    p.cfg.Model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	}
	if native {
		body["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "result",
				"schema": json.RawMessage(schema),
			},
		}
	}

	data, hdr, err := httpDo(ctx, p.hc, p.baseURL()+"/chat/completions",
		map[string]string{"Authorization": "Bearer " + p.cfg.Key}, body)
	p.record(hdr)
	if err != nil {
		return corral.RunResult{}, fmt.Errorf("openai: %w", err)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return corral.RunResult{}, fmt.Errorf("openai: decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return corral.RunResult{}, fmt.Errorf("openai: empty choices")
	}
	return corral.RunResult{Text: out.Choices[0].Message.Content, Provider: p.Name()}, nil
}

func (p *openaiProvider) record(h http.Header) {
	s := rateLimitStatus("requests", h, "x-ratelimit-limit-requests", "x-ratelimit-remaining-requests")
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
func (p *openaiProvider) Usage(_ context.Context) (*corral.LimitStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastLim, nil
}
