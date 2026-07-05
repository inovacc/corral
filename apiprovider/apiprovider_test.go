package apiprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNew_RejectsBadConfig(t *testing.T) {
	cases := []Config{
		{Format: "openai", Model: "", Key: "k"},      // no model
		{Format: "openai", Model: "gpt-4o", Key: ""}, // no key
		{Format: "nope", Model: "m", Key: "k"},       // unknown format
	}
	for i, c := range cases {
		if _, err := New(c); err == nil {
			t.Errorf("case %d: New(%+v) = nil error, want error", i, c)
		}
	}
}

func TestHTTPDo_PostsJSONAndReadsBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("X-Test"); got != "yes" {
			t.Errorf("X-Test = %q, want yes", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	body, hdr, err := httpDo(context.Background(), ts.Client(), ts.URL,
		map[string]string{"X-Test": "yes"}, map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("httpDo: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %s", body)
	}
	if hdr.Get("Content-Type") != "application/json" {
		t.Errorf("missing response header")
	}
}

func TestHTTPDo_Non2xxIsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`rate limited`))
	}))
	defer ts.Close()
	if _, _, err := httpDo(context.Background(), ts.Client(), ts.URL, nil, nil); err == nil {
		t.Fatal("want error on 429")
	}
}

func TestHTTPDo_HonorsContextCancel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := httpDo(ctx, ts.Client(), ts.URL, nil, nil); err == nil {
		t.Fatal("want error from cancelled ctx")
	}
}

func TestRateLimitStatus(t *testing.T) {
	h := http.Header{}
	h.Set("x-ratelimit-limit-requests", "100")
	h.Set("x-ratelimit-remaining-requests", "75")
	s := rateLimitStatus("requests", h, "x-ratelimit-limit-requests", "x-ratelimit-remaining-requests")
	if s == nil || len(s.Windows) != 1 {
		t.Fatalf("status = %+v", s)
	}
	if got := s.Windows[0].UsedPercent; got != 25 {
		t.Errorf("used = %v, want 25", got)
	}
	// Missing headers -> nil (non-blocking).
	if rateLimitStatus("requests", http.Header{}, "a", "b") != nil {
		t.Error("empty headers should yield nil status")
	}
}
