// Package openrouter is corral's OpenRouter API provider: it runs agent turns
// through the official OpenRouter Go SDK, giving access to 400+ models via
// vendor/model slugs behind an OpenAI-compatible API. It is the one corral
// package that depends on github.com/OpenRouterTeam/go-sdk; the rest of corral
// stays importable without pulling the SDK in.
package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"time"

	sdk "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/models/operations"

	"github.com/inovacc/corral"
)

// Config configures the OpenRouter backend. Key is the resolved secret (the
// caller reads os.Getenv(<name>)); Model is a vendor/model slug
// ("anthropic/claude-sonnet-4", "openai/gpt-4o", …). Routing is reserved for
// OpenRouter provider-routing options and is not yet wired end-to-end.
type Config struct {
	Model   string
	Key     string
	BaseURL string // optional; "" => the SDK default (openrouter.ai)
	Routing map[string]any
}

type provider struct {
	cfg Config
	sdk *sdk.OpenRouter
}

// New builds the OpenRouter provider. BaseURL, when set, overrides the SDK
// server (used by tests to point at a stub).
func New(cfg Config) (corral.Provider, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("openrouter: model is required")
	}
	if cfg.Key == "" {
		return nil, fmt.Errorf("openrouter: empty API key (is the key_env variable set?)")
	}
	opts := []sdk.SDKOption{
		sdk.WithSecurity(cfg.Key),
		sdk.WithClient(&http.Client{Timeout: 120 * time.Second}),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, sdk.WithServerURL(cfg.BaseURL))
	}
	return &provider{cfg: cfg, sdk: sdk.New(opts...)}, nil
}

func (p *provider) Name() string { return "openrouter" }

func (p *provider) Run(ctx context.Context, req corral.RunRequest) (corral.RunResult, error) {
	// v1: prompt-embedded schema (native response_format deferred).
	prompt := req.ComposePrompt(true)

	res, err := p.sdk.Chat.Send(ctx, components.ChatRequest{
		Model: sdk.Pointer(p.cfg.Model),
		Messages: []components.ChatMessages{
			components.CreateChatMessagesUser(components.ChatUserMessage{
				Role:    components.ChatUserMessageRoleUser,
				Content: components.CreateChatUserMessageContentStr(prompt),
			}),
		},
	}, nil)
	if err != nil {
		return corral.RunResult{}, fmt.Errorf("openrouter: %w", err)
	}
	text, err := firstChoiceText(res)
	if err != nil {
		return corral.RunResult{}, err
	}
	return corral.RunResult{Text: text, Provider: p.Name()}, nil
}

// firstChoiceText extracts the assistant's text from the first choice of a
// chat completion response.
//
// Confirmed via `go doc` against go-sdk v0.5.9 (see task-4-report.md for the
// full trace): Chat.Send returns *operations.SendChatCompletionRequestResponse
// (not *operations.SendResponse — the brief's placeholder name), which wraps
// *components.ChatResult as a union member. ChatChoice.Message is a
// components.ChatAssistantMessage (not a pointer), and its Content field is
// optionalnullable.OptionalNullable[components.ChatAssistantMessageContent] — a
// generated map[bool]*T wrapper, not a plain *string. GetOrZero() returns
// (value, wasSet); the union's Str field (*string) holds the text variant that
// the stub's plain-string JSON body deserializes into.
func firstChoiceText(res *operations.SendChatCompletionRequestResponse) (string, error) {
	if res == nil || res.ChatResult == nil || len(res.ChatResult.Choices) == 0 {
		return "", fmt.Errorf("openrouter: empty response")
	}
	content, ok := res.ChatResult.Choices[0].Message.Content.GetOrZero()
	if !ok || content.Str == nil {
		return "", fmt.Errorf("openrouter: choice has no content")
	}
	return *content.Str, nil
}
