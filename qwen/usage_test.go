package qwen

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestIsQuotaExhausted feeds representative provider error strings through the
// reactive classifier (no network) and asserts the quota/non-quota verdict.
func TestIsQuotaExhausted(t *testing.T) {
	// innerQuota simulates the decoded chat-completion failure that gets wrapped
	// by an outer run error, to prove the classifier sees through %w wrapping.
	innerQuota := errors.New(`HTTP 429: {"error":{"code":"insufficient_quota","message":"Free allocated quota exceeded"}}`)

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"qwen-oauth code", errors.New(`{"code":"insufficient_quota"}`), true},
		{"qwen-oauth message", errors.New("Free allocated quota exceeded"), true},
		{"gemini lineage", errors.New("Quota exceeded for quota metric 'Gemini 2.5 Pro Requests'"), true},
		{"grpc status", errors.New("rpc error: code = ResourceExhausted desc = RESOURCE_EXHAUSTED"), true},
		{"wrapped", fmt.Errorf("run qwen --prompt: %w", innerQuota), true},
		{"bare 429 no marker", errors.New("HTTP 429 Too Many Requests"), false},
		{"unrelated", errors.New("dial tcp: connection refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsQuotaExhausted(tc.err); got != tc.want {
				t.Errorf("IsQuotaExhausted(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestRetryAfter asserts the Retry-After / retry-after-ms extraction over the
// header forms Qwen embeds in a 429 message.
func TestRetryAfter(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantDur time.Duration
		wantOK  bool
	}{
		{"nil", nil, 0, false},
		{"seconds", errors.New("HTTP 429 insufficient_quota; Retry-After: 30"), 30 * time.Second, true},
		{"millis", errors.New("rate limited retry-after-ms: 1500 please wait"), 1500 * time.Millisecond, true},
		{"json millis", errors.New(`{"retry_after_ms":"2500"}`), 2500 * time.Millisecond, true},
		{"no header", errors.New("Free allocated quota exceeded"), 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotDur, gotOK := RetryAfter(tc.err)
			if gotOK != tc.wantOK || gotDur != tc.wantDur {
				t.Errorf("RetryAfter(%v) = (%v, %v), want (%v, %v)", tc.err, gotDur, gotOK, tc.wantDur, tc.wantOK)
			}
		})
	}

	// HTTP-date form: a far-future reset yields a positive delay, ok=true.
	// Real Retry-After dates are GMT (RFC 7231), so format a literal GMT stamp.
	future := time.Now().Add(48 * time.Hour).UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
	d, ok := RetryAfter(fmt.Errorf("HTTP 503; Retry-After: %s", future))
	if !ok || d <= 0 {
		t.Errorf("RetryAfter(http-date) = (%v, %v), want (>0, true)", d, ok)
	}
}
