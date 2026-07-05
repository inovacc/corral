package apiprovider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/inovacc/corral"
)

func TestOpenAI_Run_TextTurn(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer sk-test" {
			t.Errorf("auth = %q", auth)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello world"}}]}`))
	}))
	defer ts.Close()

	p, err := New(Config{Format: "openai", Model: "gpt-4o", Key: "sk-test", BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Run(context.Background(), corral.RunRequest{
		Agent: corral.Agent{Name: "judge", System: "be terse"},
		Input: "hi",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "hello world" || res.Provider != "openai" {
		t.Errorf("res = %+v", res)
	}
	if gotBody["model"] != "gpt-4o" {
		t.Errorf("model = %v", gotBody["model"])
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %v", gotBody["messages"])
	}
	m0 := msgs[0].(map[string]any)
	if m0["role"] != "user" || !strings.Contains(m0["content"].(string), "be terse") {
		t.Errorf("message[0] = %v (want user role with system prompt folded in)", m0)
	}
	if _, hasRF := gotBody["response_format"]; hasRF {
		t.Error("no schema requested; response_format must be absent")
	}
}

func TestOpenAI_Run_SchemaUsesResponseFormat(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"n\":1}"}}]}`))
	}))
	defer ts.Close()

	p, _ := New(Config{Format: "openai", Model: "gpt-4o", Key: "sk-test", BaseURL: ts.URL})
	res, err := p.Run(context.Background(), corral.RunRequest{
		Agent:  corral.Agent{Name: "j"},
		Input:  "give n",
		Schema: `{"type":"object","properties":{"n":{"type":"integer"}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != `{"n":1}` {
		t.Errorf("text = %q", res.Text)
	}
	rf, ok := gotBody["response_format"].(map[string]any)
	if !ok || rf["type"] != "json_schema" {
		t.Fatalf("response_format = %v", gotBody["response_format"])
	}
	// The user message must NOT also carry an embedded schema hint (native path).
	msgs := gotBody["messages"].([]any)
	if strings.Contains(msgs[0].(map[string]any)["content"].(string), "JSON Schema") {
		t.Error("schema was embedded in the prompt despite native response_format")
	}
}

func TestOpenAI_Usage_FromHeaders(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-limit-requests", "200")
		w.Header().Set("x-ratelimit-remaining-requests", "150")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer ts.Close()

	p, _ := New(Config{Format: "openai", Model: "gpt-4o", Key: "sk-test", BaseURL: ts.URL})
	ur := p.(corral.UsageReporter)

	// Before any Run: no snapshot, non-blocking.
	if s, err := ur.Usage(context.Background()); err != nil || s != nil {
		t.Fatalf("pre-run usage = %v, %v; want nil,nil", s, err)
	}
	if _, err := p.Run(context.Background(), corral.RunRequest{Agent: corral.Agent{Name: "j"}, Input: "x"}); err != nil {
		t.Fatal(err)
	}
	s, err := ur.Usage(context.Background())
	if err != nil || s == nil {
		t.Fatalf("post-run usage = %v, %v", s, err)
	}
	if got := s.Worst(); got != 25 {
		t.Errorf("worst used = %v, want 25", got)
	}
}

func TestOpenAI_Run_ContextCancel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	p, _ := New(Config{Format: "openai", Model: "gpt-4o", Key: "sk-test", BaseURL: ts.URL})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Run(ctx, corral.RunRequest{Agent: corral.Agent{Name: "j"}, Input: "x"}); err == nil {
		t.Fatal("want error from cancelled ctx")
	}
}
