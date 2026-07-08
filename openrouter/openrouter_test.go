package openrouter

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

func TestNew_RejectsBadConfig(t *testing.T) {
	if _, err := New(Config{Model: "", Key: "k"}); err == nil {
		t.Error("want error on empty model")
	}
	if _, err := New(Config{Model: "m", Key: ""}); err == nil {
		t.Error("want error on empty key")
	}
}

func TestRun_MapsTurn(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		// OpenAI-compatible chat completion body the SDK deserializes.
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"routed hello"}}]}`))
	}))
	defer ts.Close()

	p, err := New(Config{Model: "anthropic/claude-sonnet-4", Key: "sk-or", BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Run(context.Background(), corral.RunRequest{
		Agent: corral.Agent{Name: "j", System: "be brief"},
		Input: "hi",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "routed hello" || res.Provider != "openrouter" {
		t.Errorf("res = %+v", res)
	}
	if gotBody["model"] != "anthropic/claude-sonnet-4" {
		t.Errorf("model = %v", gotBody["model"])
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) == 0 || !strings.Contains(msgs[0].(map[string]any)["content"].(string), "be brief") {
		t.Errorf("system prompt not folded into the turn: %v", gotBody["messages"])
	}
}

func TestRun_WrapsAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer ts.Close()
	p, _ := New(Config{Model: "m", Key: "sk-or", BaseURL: ts.URL})
	_, err := p.Run(context.Background(), corral.RunRequest{Agent: corral.Agent{Name: "j"}, Input: "x"})
	if err == nil {
		t.Fatal("want error on 401")
	}
	if !strings.Contains(err.Error(), "openrouter") {
		t.Errorf("error not wrapped with provider name: %v", err)
	}
}
