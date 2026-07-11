package airef

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// ChatRequest is a one-shot system+user chat turn.
type ChatRequest struct {
	System      string
	User        string
	MaxTokens   int
	Temperature float64
	Effort      string // reasoning effort; "" omits the provider's effort control
}

// Chat sends req to provider p using model (empty => p.DefaultModel), authed with
// key. Effort maps to each provider's control: Anthropic output_config.effort,
// OpenAI/OpenRouter reasoning_effort, Gemini thinkingConfig.thinkingBudget.
// Errors are kept generic (Gemini carries the key in the URL).
func Chat(ctx context.Context, p Provider, model, key string, req ChatRequest) (string, error) {
	if key == "" {
		return "", fmt.Errorf("no API key")
	}
	if model == "" {
		model = p.DefaultModel
	}
	switch p.family {
	case "anthropic":
		return chatAnthropic(ctx, model, key, req)
	case "gemini":
		return chatGemini(ctx, model, key, req)
	default: // openai + openrouter
		base := "https://api.openai.com/v1"
		if p.Name == "openrouter" {
			base = "https://openrouter.ai/api/v1"
		}
		return chatOpenAI(ctx, base, model, key, req)
	}
}

func postJSON(ctx context.Context, url string, headers map[string]string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build request")
	}
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return fmt.Errorf("request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(data, out)
}

func chatOpenAI(ctx context.Context, base, model, key string, req ChatRequest) (string, error) {
	body := map[string]any{
		"model":       model,
		"temperature": req.Temperature,
		"max_tokens":  req.MaxTokens,
		"messages": []map[string]string{
			{"role": "system", "content": req.System},
			{"role": "user", "content": req.User},
		},
	}
	if req.Effort != "" {
		body["reasoning_effort"] = req.Effort // ignored by non-reasoning models
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := postJSON(ctx, base+"/chat/completions", map[string]string{"Authorization": "Bearer " + key}, body, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func chatAnthropic(ctx context.Context, model, key string, req ChatRequest) (string, error) {
	body := map[string]any{
		"model":      model,
		"max_tokens": req.MaxTokens,
		"system":     req.System,
		"messages":   []map[string]string{{"role": "user", "content": req.User}},
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.Effort != "" {
		body["output_config"] = map[string]any{"effort": req.Effort} // GA; pair with adaptive thinking
	}
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	headers := map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}
	if err := postJSON(ctx, "https://api.anthropic.com/v1/messages", headers, body, &out); err != nil {
		return "", err
	}
	if len(out.Content) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return strings.TrimSpace(out.Content[0].Text), nil
}

func chatGemini(ctx context.Context, model, key string, req ChatRequest) (string, error) {
	url := "https://generativelanguage.googleapis.com/v1beta/models/" + model + ":generateContent?key=" + key
	// 2.5 models are "thinking" models: budget 0 disables (deterministic short
	// answers), -1 is dynamic, a positive int is a token budget. Effort accepts
	// a numeric budget or "dynamic"; anything else falls back to disabled.
	budget := 0
	switch {
	case req.Effort == "dynamic":
		budget = -1
	case req.Effort != "":
		if n, err := strconv.Atoi(req.Effort); err == nil {
			budget = n
		}
	}
	body := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": req.System + "\n\n" + req.User}}},
		},
		"generationConfig": map[string]any{
			"temperature":     req.Temperature,
			"maxOutputTokens": req.MaxTokens,
			"thinkingConfig":  map[string]any{"thinkingBudget": budget},
		},
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}
	if err := postJSON(ctx, url, nil, body, &out); err != nil {
		return "", err
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		reason := ""
		if len(out.Candidates) > 0 {
			reason = out.Candidates[0].FinishReason
		}
		return "", fmt.Errorf("empty response (finishReason=%s)", reason)
	}
	return strings.TrimSpace(out.Candidates[0].Content.Parts[0].Text), nil
}
