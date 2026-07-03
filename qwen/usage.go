package qwen

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Reactive quota signals — Qwen exposes NO remote usage/quota endpoint.
//
// Unlike the claude/codex/agy providers (each of which implements
// corral.UsageReporter via a real GET against a subscription-usage endpoint),
// Qwen Code — a gemini-cli fork — ships NO queryable "/usage" or "/quota"
// route: an exhaustive grep of the shipped v0.19.6 bundle finds no
// getUsage / fetchQuota / GET /quota / /entitlement / /subscription call
// (see docs/kb/qwen.md). Consumption is observable only two ways:
//
//  1. Local token accounting — the OpenAI-compatible chat completion's `usage`
//     block (prompt/completion/total tokens) summed client-side. The API
//     returns NO server-side remaining/limit/balance field to poll.
//  2. Reactively — exhaustion is read off the chat call's HTTP *error*
//     (429 / RESOURCE_EXHAUSTED), and the reset window off the Retry-After /
//     retry-after-ms response header.
//
// Because there is no proactive percent to watch, this package deliberately
// does NOT implement corral.UsageReporter and qwen.go stays a bare
// CLIProvider preset — proactive percent-based monitoring is unavailable in
// headless mode. The helpers below let a corral host recognise quota
// exhaustion *after* a failed run instead. (ACP-mode usage blocks may add a
// proactive path in a future revision.)

// quotaMarkers are the substrings (lower-cased) that identify a Qwen/Gemini
// quota-exhaustion error — the ONLY "limit reached" signal Qwen emits. qwen-oauth
// carries code "insufficient_quota" + message "free allocated quota exceeded";
// the gemini-cli lineage surfaces "Quota exceeded for quota metric ...", and the
// gRPC/status form is "RESOURCE_EXHAUSTED". Matched case-insensitively.
var quotaMarkers = []string{
	"insufficient_quota",
	"free allocated quota exceeded",
	"quota exceeded for quota metric",
	"resource_exhausted",
}

// IsQuotaExhausted reports whether err (typically a wrapped chat-completion run
// failure) is a Qwen/Gemini quota-exhaustion signal. Since Qwen has no proactive
// remaining/limit field, this reactive 429 classifier is a host's only way to
// know the quota is spent. It is tolerant: a nil error, or any error whose text
// lacks the markers, is false; it never panics, matches case-insensitively, and
// reads err.Error() so it sees through fmt.Errorf(%w) wrapping. A bare "HTTP 429"
// without a quota marker is intentionally NOT treated as exhaustion.
func IsQuotaExhausted(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, m := range quotaMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// retryAfter* match a reset delay embedded in an error message, mirroring the
// Retry-After / retry-after-ms response headers Qwen's retry layer reads on
// 429/503. The -ms form is checked first so it is not swallowed by the seconds
// form. All are case-insensitive and tolerant of ":"/"="/quote/space separators.
var (
	retryAfterMsRe   = regexp.MustCompile(`(?i)retry[-_ ]?after[-_ ]?ms["'\s:=]+(\d+)`)
	retryAfterSecRe  = regexp.MustCompile(`(?i)retry[-_ ]?after["'\s:=]+(\d+)`)
	retryAfterDateRe = regexp.MustCompile(`(?i)retry[-_ ]?after["'\s:=]+([A-Za-z]{3},[^"'\n\r]+?GMT)`)
)

// RetryAfter extracts the reset delay embedded in a Qwen quota/rate-limit error,
// mirroring the Retry-After / retry-after-ms headers Qwen reads on 429/503. It is
// tolerant: it returns (0, false) when no such value is present (or err is nil)
// and never panics. Recognised forms: "retry-after-ms: 30000" (milliseconds),
// "Retry-After: 30" (seconds), and an HTTP-date "Retry-After: Wed, 21 Oct 2026
// 07:28:00 GMT" (delay until then, clamped at 0).
func RetryAfter(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	msg := err.Error()
	if m := retryAfterMsRe.FindStringSubmatch(msg); m != nil {
		if n, e := strconv.Atoi(m[1]); e == nil {
			return time.Duration(n) * time.Millisecond, true
		}
	}
	if m := retryAfterSecRe.FindStringSubmatch(msg); m != nil {
		if n, e := strconv.Atoi(m[1]); e == nil {
			return time.Duration(n) * time.Second, true
		}
	}
	if m := retryAfterDateRe.FindStringSubmatch(msg); m != nil {
		if t, perr := time.Parse(time.RFC1123, strings.TrimSpace(m[1])); perr == nil {
			d := time.Until(t)
			if d < 0 {
				d = 0
			}
			return d, true
		}
	}
	return 0, false
}
