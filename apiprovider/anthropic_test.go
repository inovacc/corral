package apiprovider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/inovacc/corral"
)

func TestAnthropic_Run_TextTurn(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "sk-ant" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version header")
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hi there"}]}`))
	}))
	defer ts.Close()

	p, _ := New(Config{Format: "anthropic", Model: "claude-sonnet-4", Key: "sk-ant", BaseURL: ts.URL})
	res, err := p.Run(context.Background(), corral.RunRequest{Agent: corral.Agent{Name: "j"}, Input: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hi there" || res.Provider != "anthropic" {
		t.Errorf("res = %+v", res)
	}
	if gotBody["max_tokens"] == nil {
		t.Error("max_tokens must be set (anthropic requires it)")
	}
	if _, hasTools := gotBody["tools"]; hasTools {
		t.Error("no schema requested; tools must be absent")
	}
}

func TestAnthropic_Run_SchemaUsesToolUse(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"tool_use","name":"result","input":{"n":7}}]}`))
	}))
	defer ts.Close()

	p, _ := New(Config{Format: "anthropic", Model: "claude-sonnet-4", Key: "sk-ant", BaseURL: ts.URL})
	res, err := p.Run(context.Background(), corral.RunRequest{
		Agent:  corral.Agent{Name: "j"},
		Input:  "give n",
		Schema: `{"type":"object","properties":{"n":{"type":"integer"}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]int
	if err := json.Unmarshal([]byte(res.Text), &parsed); err != nil || parsed["n"] != 7 {
		t.Errorf("text = %q (want JSON {\"n\":7})", res.Text)
	}
	if _, ok := gotBody["tools"]; !ok {
		t.Error("schema requested; tools must be present")
	}
	tc, _ := gotBody["tool_choice"].(map[string]any)
	if tc["type"] != "tool" || tc["name"] != "result" {
		t.Errorf("tool_choice = %v", gotBody["tool_choice"])
	}
}

func TestAnthropic_Usage_FromHeaders(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("anthropic-ratelimit-requests-limit", "50")
		w.Header().Set("anthropic-ratelimit-requests-remaining", "40")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer ts.Close()
	p, _ := New(Config{Format: "anthropic", Model: "claude-sonnet-4", Key: "sk-ant", BaseURL: ts.URL})
	if _, err := p.Run(context.Background(), corral.RunRequest{Agent: corral.Agent{Name: "j"}, Input: "x"}); err != nil {
		t.Fatal(err)
	}
	s, err := p.(corral.UsageReporter).Usage(context.Background())
	if err != nil || s == nil || s.Worst() != 20 {
		t.Fatalf("usage = %+v, %v (want worst 20)", s, err)
	}
}
