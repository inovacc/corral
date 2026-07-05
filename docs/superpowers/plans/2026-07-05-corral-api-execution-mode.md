# corral `api` Execution Mode + API Providers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a corral component run agent turns through a metered API backend (OpenRouter via the official Go SDK, or hand-rolled OpenAI / Anthropic) instead of only a subscription CLI, selected per vendor by config.

**Architecture:** Add API-based `corral.Provider` backends in two new packages — `apiprovider/` (hand-rolled OpenAI + Anthropic, zero new deps) and `openrouter/` (go-sdk-backed). Each is a plain `Provider` (+ optional `UsageReporter`), so it drops into the existing Agency / SessionPool / monitor unchanged. The `aihost` generator learns a per-vendor `execution` config and emits a `New()` that constructs the chosen API backend when mode is `api`, or the existing subscription path otherwise.

**Tech Stack:** Go 1.26.x, stdlib `net/http` + `encoding/json` for the hand-rolled backends, `github.com/OpenRouterTeam/go-sdk@v0.5.9` (Apache-2.0, beta, requires Go 1.25+) for OpenRouter. Tests use `net/http/httptest` (hand-rolled) and the SDK's `WithServerURL` (OpenRouter) — **no live network, no real keys**.

## Global Constraints

- **Branch:** `feat/corral-api-execution-mode` (already checked out; spec committed at `64893c9`).
- **Module path:** `github.com/inovacc/corral`; new packages are `github.com/inovacc/corral/apiprovider` and `github.com/inovacc/corral/openrouter`.
- **Dependency thesis amendment:** corral moves from one external dep (`conpty`) to **two** — `conpty` + `github.com/OpenRouterTeam/go-sdk v0.5.9`. This is a deliberate, recorded decision (Task 8). The `apiprovider` package adds **no** further deps (stdlib only).
- **`key_env` is an environment-variable NAME, never a literal key.** IR validation rejects any `key_env` that is not a valid env-var identifier (`^[A-Za-z_][A-Za-z0-9_]*$`). The generated code resolves the secret at runtime via `os.Getenv(<name>)`; a literal key is never baked into generated source.
- **Backward compatibility (MANDATORY deprecation policy):** the `execution` config is **additive**. Absent `execution` for a vendor ⇒ `subscription` mode (today's behavior). Existing `corral.json` files keep working unchanged. The existing `vendors: []string` field is untouched; `execution` is a new top-level object keyed by vendor name.
- **No secrets in logs or errors.** Backends never include the API key in error text.
- **`Provider` seam (do not change):** `Name() string`; `Run(ctx context.Context, req RunRequest) (RunResult, error)`. `RunRequest{Agent, Input, Dir, Schema}` with `ComposePrompt(schemaHint bool) string` and `EffectiveSchema() string`. `RunResult{Text, Provider}`. `UsageReporter{ Usage(ctx context.Context) (*LimitStatus, error) }` is optional.
- **TDD, DRY, YAGNI, frequent commits.** Every task ends green (`go test ./...`) and committed. Commit style: conventional commits, **no** AI-attribution / Co-Authored-By trailer. Use `git config user.name="Dyam Marcano"` / `user.email="dyam.marcano@gmail.com"`.
- **Deferred to BACKLOG (do NOT build now):** streaming; embeddings/rerank/TTS; OpenRouter native `response_format` structured output (v1 uses prompt-embedded schema); OpenRouter `routing` codegen in the generator (the `Routing` field exists on the config for direct-Go callers but the generator does not emit it yet); retrofitting existing subscription providers.

## File Structure

| File | Responsibility |
|---|---|
| `apiprovider/apiprovider.go` (new) | `Config`, `New(cfg) (corral.Provider, error)` format-dispatch, shared `httpDo` ctx-aware POST helper, `rateLimitStatus` header parser. |
| `apiprovider/openai.go` (new) | OpenAI Chat Completions backend: `Run` (text + `response_format` schema), `UsageReporter` from `x-ratelimit-*` headers. |
| `apiprovider/anthropic.go` (new) | Anthropic Messages backend: `Run` (text + tool-use schema), `UsageReporter` from `anthropic-ratelimit-*` headers. |
| `apiprovider/apiprovider_test.go`, `openai_test.go`, `anthropic_test.go` (new) | `httptest`-driven request/response/ctx-cancel/usage tests. |
| `openrouter/openrouter.go` (new) | go-sdk-backed backend: `Config`, `New(cfg) (corral.Provider, error)`, `Run` via `s.Chat.Send`, `Name`. |
| `openrouter/openrouter_test.go` (new) | `WithServerURL(httptest)` stub: turn mapping + typed-error wrap. |
| `agency.go` (modify) | Add `NewAgencyWithProvider(p Provider, dir string) *Agency`; refactor `NewAgency` to call it. |
| `agency_apiprovider_test.go` (new) | Unit test for `NewAgencyWithProvider` (explicit-provider construction, no-pool for one-shot). |
| `aihost/ir.go` (modify) | `Execution` / `ExecutionConfig` types; `Component.Executions`; `Load` parses + validates the `execution` object. |
| `aihost/ir_test.go` (modify) | Parse + validation tests (good, unknown mode, unknown format, literal-key `key_env`, absent ⇒ subscription). |
| `aihost/gen.go` (modify) | `componentGenData` gains `ExtraImports` + `NewFunc`; `moduleWrapperFiles` builds the api-vs-subscription `New()` body from `c.Executions[vendor]`. |
| `aihost/gen_test.go` (modify) | Golden asserts: api vendor emits the backend construction; subscription vendor unchanged. |
| `go.mod` / `go.sum` (modify) | Add `github.com/OpenRouterTeam/go-sdk v0.5.9`. |
| `CLAUDE.md` (modify) | Record the two-dep thesis amendment (rev bump). |
| `docs/BACKLOG.md` (modify) | Mark `#1` progress; log the deferred items. |

---

### Task 1: `apiprovider` foundation — Config, ctx-aware HTTP helper, rate-limit parser

**Files:**
- Create: `apiprovider/apiprovider.go`
- Create: `apiprovider/apiprovider_test.go`

**Interfaces:**
- Consumes: `github.com/inovacc/corral` (`corral.LimitStatus`, `corral.LimitWindow`).
- Produces:
  - `type Config struct { Format, Model, BaseURL, Key string }`
  - `func New(cfg Config) (corral.Provider, error)` — dispatch on `cfg.Format` (`"openai"` / `"anthropic"`); errors on unknown format, empty `Model`, or empty `Key`.
  - `func httpDo(ctx context.Context, hc *http.Client, url string, headers map[string]string, body any) (respBody []byte, respHeader http.Header, err error)` — marshals `body` to JSON, POSTs with ctx, returns raw body + headers; returns a non-nil error (with body appended, key never included) on non-2xx.
  - `func rateLimitStatus(window string, h http.Header, limitHdr, remainingHdr string) *corral.LimitStatus` — `nil` unless both headers parse and limit > 0; else a one-window `LimitStatus` with `UsedPercent = (limit-remaining)/limit*100`.

- [ ] **Step 1: Write the failing test**

Create `apiprovider/apiprovider_test.go`:

```go
package apiprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNew_RejectsBadConfig(t *testing.T) {
	cases := []Config{
		{Format: "openai", Model: "", Key: "k"},          // no model
		{Format: "openai", Model: "gpt-4o", Key: ""},      // no key
		{Format: "nope", Model: "m", Key: "k"},            // unknown format
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apiprovider/`
Expected: FAIL — package/functions do not exist (`New`, `httpDo`, `rateLimitStatus`, `Config` undefined).

- [ ] **Step 3: Write minimal implementation**

Create `apiprovider/apiprovider.go`:

```go
// Package apiprovider provides metered-API corral.Provider backends for
// OpenAI-compatible Chat Completions and Anthropic Messages, hand-rolled on the
// standard library (no third-party deps). Each backend is a plain corral.Provider
// plus an optional corral.UsageReporter, so it drops into the Agency, SessionPool,
// and monitor unchanged. OpenRouter has its own package (github.com/inovacc/corral/openrouter).
package apiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/inovacc/corral"
)

// Config selects and configures a hand-rolled API backend. Key is the resolved
// secret (the caller reads os.Getenv(<name>) — this package never sees the env
// name, only the value, and never logs it).
type Config struct {
	Format  string // "openai" | "anthropic"
	Model   string
	BaseURL string // optional; defaults per format
	Key     string // resolved secret
}

// New returns the hand-rolled backend for cfg.Format.
func New(cfg Config) (corral.Provider, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("apiprovider: model is required")
	}
	if cfg.Key == "" {
		return nil, fmt.Errorf("apiprovider: empty API key (is the key_env variable set?)")
	}
	switch cfg.Format {
	case "openai":
		return newOpenAI(cfg), nil
	case "anthropic":
		return newAnthropic(cfg), nil
	default:
		return nil, fmt.Errorf("apiprovider: unknown format %q (want openai|anthropic)", cfg.Format)
	}
}

// defaultClient bounds every backend call; httptest URLs are reachable with it.
func defaultClient() *http.Client { return &http.Client{Timeout: 120 * time.Second} }

// httpDo POSTs body as JSON to url with headers, honoring ctx, and returns the
// raw response body + headers. A non-2xx status is an error (body appended for
// diagnostics; the request key is a header, never echoed into the message).
func httpDo(ctx context.Context, hc *http.Client, url string, headers map[string]string, body any) ([]byte, http.Header, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("apiprovider: marshal request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, rdr)
	if err != nil {
		return nil, nil, fmt.Errorf("apiprovider: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("apiprovider: request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, fmt.Errorf("apiprovider: read body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return data, resp.Header, fmt.Errorf("apiprovider: status %d: %s", resp.StatusCode, truncate(data, 300))
	}
	return data, resp.Header, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// rateLimitStatus builds a one-window LimitStatus from a limit/remaining header
// pair, or nil when either is absent/unparseable (absence never blocks the fleet).
func rateLimitStatus(window string, h http.Header, limitHdr, remainingHdr string) *corral.LimitStatus {
	limit, lok := atoi(h.Get(limitHdr))
	remaining, rok := atoi(h.Get(remainingHdr))
	if !lok || !rok || limit <= 0 {
		return nil
	}
	used := float64(limit-remaining) / float64(limit) * 100
	return &corral.LimitStatus{Windows: []corral.LimitWindow{{Name: window, UsedPercent: used}}}
}

func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./apiprovider/`
Expected: PASS (all Task-1 tests). `New` for `openai`/`anthropic` won't be reachable yet because `newOpenAI`/`newAnthropic` don't exist — so **temporarily** the `New` switch will not compile. To keep this task self-contained, add minimal stub constructors at the bottom of `apiprovider.go` returning a zero provider is NOT allowed (must return a real `corral.Provider`). Instead, defer the `case "openai"/"anthropic"` bodies: implement them here as thin placeholders that Tasks 2–3 flesh out. Add these stubs now so the package compiles:

```go
// placeholder backends fleshed out in openai.go / anthropic.go
func newOpenAI(cfg Config) corral.Provider    { return &openaiProvider{cfg: cfg, hc: defaultClient()} }
func newAnthropic(cfg Config) corral.Provider { return &anthropicProvider{cfg: cfg, hc: defaultClient()} }
```

Because `openaiProvider` / `anthropicProvider` types live in Tasks 2–3, this task cannot compile alone. **Right-sizing decision:** fold the *type declarations* (struct + `Name()` + a not-yet-wired `Run` returning `fmt.Errorf("not implemented")`) into this task so it compiles and its own tests pass, then Tasks 2–3 replace the `Run`/`Usage` bodies. Add to `apiprovider.go`:

```go
type openaiProvider struct {
	cfg Config
	hc  *http.Client
	mu  syncMutex
}
type anthropicProvider struct {
	cfg Config
	hc  *http.Client
	mu  syncMutex
}

func (p *openaiProvider) Name() string    { return "openai" }
func (p *anthropicProvider) Name() string { return "anthropic" }
```

Replace `syncMutex` with a real `sync.Mutex` field (import `sync`) — written literally as:

```go
	mu       sync.Mutex
	lastLim  *corral.LimitStatus
```

and give both structs a temporary `Run` so the package builds:

```go
func (p *openaiProvider) Run(ctx context.Context, req corral.RunRequest) (corral.RunResult, error) {
	return corral.RunResult{}, fmt.Errorf("openai: not implemented")
}
func (p *anthropicProvider) Run(ctx context.Context, req corral.RunRequest) (corral.RunResult, error) {
	return corral.RunResult{}, fmt.Errorf("anthropic: not implemented")
}
```

Re-run `go test ./apiprovider/` → PASS.

- [ ] **Step 5: Commit**

```bash
git add apiprovider/apiprovider.go apiprovider/apiprovider_test.go
git commit -m "feat(apiprovider): config, ctx-aware http helper, rate-limit parser"
```

---

### Task 2: OpenAI backend — Run (text + response_format) + UsageReporter

**Files:**
- Create: `apiprovider/openai.go`
- Create: `apiprovider/openai_test.go`
- Modify: `apiprovider/apiprovider.go` (remove the temporary `openaiProvider` struct/methods added in Task 1; they move to `openai.go`)

**Interfaces:**
- Consumes: `Config`, `httpDo`, `rateLimitStatus`, `defaultClient` (Task 1); `corral.RunRequest.ComposePrompt`, `corral.RunRequest.EffectiveSchema`.
- Produces: `openaiProvider` implementing `corral.Provider` + `corral.UsageReporter`. Endpoint `POST {base}/chat/completions` (base default `https://api.openai.com/v1`), `Authorization: Bearer <key>`. Structured output via `response_format:{type:"json_schema",...}` when the request has a schema (so the prompt is NOT schema-embedded). Text extracted from `choices[0].message.content`.

- [ ] **Step 1: Write the failing test**

Create `apiprovider/openai_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apiprovider/ -run TestOpenAI`
Expected: FAIL — `Run` returns `"openai: not implemented"` (Task-1 stub), so text/usage assertions fail.

- [ ] **Step 3: Write minimal implementation**

Delete the temporary `openaiProvider` struct + `Name`/`Run` from `apiprovider.go` (added in Task 1). Create `apiprovider/openai.go`:

```go
package apiprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/inovacc/corral"
)

type openaiProvider struct {
	cfg     Config
	hc      *httpClient
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

	data, hdr, err := httpDo(ctx, p.hc.c, p.baseURL()+"/chat/completions",
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

func (p *openaiProvider) record(h interface{ Get(string) string }) {
	if h == nil {
		return
	}
	s := rateLimitStatusFromGetter(h, "requests", "x-ratelimit-limit-requests", "x-ratelimit-remaining-requests")
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
```

**Note on `p.hc`:** Task 1's stub gave the struct `hc *http.Client`. Keep that exact field. Replace `p.hc.c` above with `p.hc` and the field type `*http.Client`; drop the `*httpClient` wrapper (it does not exist). Corrected struct + helper:

```go
type openaiProvider struct {
	cfg     Config
	hc      *http.Client
	mu      sync.Mutex
	lastLim *corral.LimitStatus
}
```
and the call: `httpDo(ctx, p.hc, p.baseURL()+"/chat/completions", ...)`. Import `net/http`.

Because `record` takes response headers, simplify by passing `http.Header` directly (that is what `httpDo` returns). Replace `record` + the getter indirection with:

```go
func (p *openaiProvider) record(h http.Header) {
	s := rateLimitStatus("requests", h, "x-ratelimit-limit-requests", "x-ratelimit-remaining-requests")
	if s == nil {
		return
	}
	p.mu.Lock()
	p.lastLim = s
	p.mu.Unlock()
}
```

(Delete the `rateLimitStatusFromGetter` reference — `rateLimitStatus` from Task 1 already takes `http.Header`.) Ensure `newOpenAI` in `apiprovider.go` reads: `func newOpenAI(cfg Config) corral.Provider { return &openaiProvider{cfg: cfg, hc: defaultClient()} }`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./apiprovider/ -run TestOpenAI`
Expected: PASS. Then `go test ./apiprovider/` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add apiprovider/openai.go apiprovider/openai_test.go apiprovider/apiprovider.go
git commit -m "feat(apiprovider): openai backend — run, response_format schema, usage headers"
```

---

### Task 3: Anthropic backend — Run (text + tool-use schema) + UsageReporter

**Files:**
- Create: `apiprovider/anthropic.go`
- Create: `apiprovider/anthropic_test.go`
- Modify: `apiprovider/apiprovider.go` (remove the temporary `anthropicProvider` struct/methods from Task 1)

**Interfaces:**
- Consumes: same Task-1 helpers; `corral.RunRequest`.
- Produces: `anthropicProvider` implementing `corral.Provider` + `corral.UsageReporter`. Endpoint `POST {base}/v1/messages` (base default `https://api.anthropic.com`), headers `x-api-key: <key>`, `anthropic-version: 2023-06-01`. Required `max_tokens` defaulted to `4096`. Structured output via a single forced tool (`tools:[{name:"result",input_schema:<schema>}]` + `tool_choice:{type:"tool",name:"result"}`); the tool-use block's `input` is marshaled to `RunResult.Text`. Text mode reads `content[0].text`. Usage from `anthropic-ratelimit-requests-limit` / `-remaining`.

- [ ] **Step 1: Write the failing test**

Create `apiprovider/anthropic_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./apiprovider/ -run TestAnthropic`
Expected: FAIL — `anthropicProvider.Run` is the Task-1 not-implemented stub.

- [ ] **Step 3: Write minimal implementation**

Delete the temporary `anthropicProvider` struct + methods from `apiprovider.go`. Create `apiprovider/anthropic.go`:

```go
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

func (p *anthropicProvider) Usage(_ context.Context) (*corral.LimitStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastLim, nil
}
```

Confirm `newAnthropic` in `apiprovider.go`: `func newAnthropic(cfg Config) corral.Provider { return &anthropicProvider{cfg: cfg, hc: defaultClient()} }`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./apiprovider/`
Expected: PASS (all Task 1–3 tests).

- [ ] **Step 5: Commit**

```bash
git add apiprovider/anthropic.go apiprovider/anthropic_test.go apiprovider/apiprovider.go
git commit -m "feat(apiprovider): anthropic backend — run, tool-use schema, usage headers"
```

---

### Task 4: `openrouter` package — go-sdk backend + `go.mod` dependency

**Files:**
- Create: `openrouter/openrouter.go`
- Create: `openrouter/openrouter_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: `github.com/OpenRouterTeam/go-sdk` (`openrouter.New`, `openrouter.WithSecurity`, `openrouter.WithServerURL`, `openrouter.WithClient`, `openrouter.Pointer`), `.../go-sdk/models/components` (`ChatRequest`, `ChatMessages`, `CreateChatMessagesUser`, `ChatUserMessage`, `ChatUserMessageRoleUser`, `CreateChatUserMessageContentStr`), `.../go-sdk/models/operations` (the `Send` response type), `.../go-sdk/models/sdkerrors`.
- Produces: `type Config struct { Model, Key, BaseURL string; Routing map[string]any }`; `func New(cfg Config) (corral.Provider, error)`; a `provider` implementing `corral.Provider` with `Name() == "openrouter"`. v1 uses prompt-embedded schema (`ComposePrompt(true)`) — native `response_format` is deferred (BACKLOG).

> **SDK-shape note (confirm at GREEN):** the exact response accessor (`res.ChatResult.Choices[0].Message.Content` and its Go type) and the `Send` return type name (`*operations.SendResponse`) are from the published example; the stub test (below) drives a real JSON body through the SDK, so the accessor MUST match the generated types for the test to pass. If it does not compile, run `go doc github.com/OpenRouterTeam/go-sdk/models/operations` and `go doc github.com/OpenRouterTeam/go-sdk/models/components.ChatResult` and adjust the extraction line — do not change the test's asserted behavior.

- [ ] **Step 1: Add the dependency**

Run:
```bash
go get github.com/OpenRouterTeam/go-sdk@v0.5.9
```
Expected: `go.mod` gains `require github.com/OpenRouterTeam/go-sdk v0.5.9`; `go.sum` updated.

- [ ] **Step 2: Write the failing test**

Create `openrouter/openrouter_test.go`:

```go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./openrouter/`
Expected: FAIL — package does not exist.

- [ ] **Step 4: Write minimal implementation**

Create `openrouter/openrouter.go`:

```go
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
```

Add the extraction helper in the same file. **Confirm the accessor via `go doc` before finalizing** — write it to satisfy the stub body `{"choices":[{"message":{"content":"routed hello"}}]}`:

```go
func firstChoiceText(res *operations.SendResponse) (string, error) {
	if res == nil || res.ChatResult == nil || len(res.ChatResult.Choices) == 0 {
		return "", fmt.Errorf("openrouter: empty response")
	}
	msg := res.ChatResult.Choices[0].Message
	// Message.Content may be *string in the generated types; adjust per `go doc`.
	if msg.Content == nil {
		return "", fmt.Errorf("openrouter: choice has no content")
	}
	return *msg.Content, nil
}
```

Add `"github.com/OpenRouterTeam/go-sdk/models/operations"` to the import block. If `go doc` shows `Send` returns a differently-named type or `Content` is a non-pointer/union, adjust `firstChoiceText` (and its import) accordingly — the test's asserted text (`"routed hello"`) does not change.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./openrouter/`
Expected: PASS. Then `go build ./...` → clean.

- [ ] **Step 6: Commit**

```bash
git add openrouter/openrouter.go openrouter/openrouter_test.go go.mod go.sum
git commit -m "feat(openrouter): go-sdk-backed api provider + pin go-sdk v0.5.9"
```

---

### Task 5: `NewAgencyWithProvider` — construct an Agency around an explicit provider

**Files:**
- Modify: `agency.go`
- Create: `agency_apiprovider_test.go`

**Interfaces:**
- Produces: `func NewAgencyWithProvider(p Provider, dir string) *Agency` — sets `Provider`, `Dir`, `LimitThreshold = DefaultLimitThreshold`, and a `SessionPool` only when `p` implements `SessionOpener` (API backends are one-shot, so no pool). `NewAgency` is refactored to delegate to it.

- [ ] **Step 1: Write the failing test**

Create `agency_apiprovider_test.go`:

```go
package corral

import (
	"context"
	"testing"
)

// apiLikeProvider is a one-shot Provider (NOT a SessionOpener), modeling an API
// backend.
type apiLikeProvider struct{ ran bool }

func (a *apiLikeProvider) Name() string { return "api-like" }
func (a *apiLikeProvider) Run(_ context.Context, _ RunRequest) (RunResult, error) {
	a.ran = true
	return RunResult{Text: "ok", Provider: "api-like"}, nil
}

func TestNewAgencyWithProvider_OneShotNoPool(t *testing.T) {
	p := &apiLikeProvider{}
	a := NewAgencyWithProvider(p, ".")
	if a.Provider != p {
		t.Fatal("provider not set")
	}
	if a.pool != nil {
		t.Error("one-shot provider must not get a session pool")
	}
	if a.LimitThreshold != DefaultLimitThreshold {
		t.Errorf("threshold = %v, want default %v", a.LimitThreshold, DefaultLimitThreshold)
	}
	res, err := a.Run(context.Background(), "quality", "in")
	// unknown agent name -> error, but provider path is what we assert via RunAgent:
	_ = res
	_ = err
	r2, err := a.RunAgent(context.Background(), Agent{Name: "x"}, "in")
	if err != nil || r2.Text != "ok" || !p.ran {
		t.Errorf("RunAgent via explicit provider: res=%+v err=%v ran=%v", r2, err, p.ran)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestNewAgencyWithProvider`
Expected: FAIL — `NewAgencyWithProvider` undefined.

- [ ] **Step 3: Write minimal implementation**

In `agency.go`, add `NewAgencyWithProvider` and refactor `NewAgency`:

```go
// NewAgencyWithProvider builds an Agency around an already-constructed Provider
// (e.g. an API backend from the apiprovider/ or openrouter/ packages, which take
// per-call config that the name registry cannot supply). Warm sessions are
// enabled only when the provider implements SessionOpener; one-shot API backends
// run through Provider.Run directly.
func NewAgencyWithProvider(p Provider, dir string) *Agency {
	a := &Agency{Provider: p, Dir: dir, LimitThreshold: DefaultLimitThreshold}
	if o, ok := p.(SessionOpener); ok {
		a.pool = NewSessionPool(o)
	}
	return a
}
```

Replace the body of `NewAgency` with a delegation:

```go
func NewAgency(provider, dir string) (*Agency, error) {
	p, err := ProviderByName(provider)
	if err != nil {
		return nil, err
	}
	return NewAgencyWithProvider(p, dir), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run 'TestNewAgencyWithProvider|TestAgency'`
Expected: PASS (new test + existing agency/monitor tests unaffected).

- [ ] **Step 5: Commit**

```bash
git add agency.go agency_apiprovider_test.go
git commit -m "feat(agency): NewAgencyWithProvider for explicit (api) provider construction"
```

---

### Task 6: aihost IR — `Execution` config, parse + validate

**Files:**
- Modify: `aihost/ir.go`
- Modify: `aihost/ir_test.go`

**Interfaces:**
- Produces:
  - `type Execution struct { Mode string; Config ExecutionConfig }`
  - `type ExecutionConfig struct { Format, Model, KeyEnv, BaseURL string; Routing map[string]any }`
  - `Component` gains `Executions map[string]Execution` (vendor name → execution; `nil`/absent ⇒ subscription).
  - `Load` parses a top-level `execution` object and validates: `Mode ∈ {"", "subscription", "api"}`; when `api`, `Format ∈ {"openrouter","openai","anthropic"}`, `Model` required, `KeyEnv` required **and** a valid env-var name (`^[A-Za-z_][A-Za-z0-9_]*$`) — a literal-looking key is rejected.

- [ ] **Step 1: Write the failing test**

Append to `aihost/ir_test.go`:

```go
func writeCfg(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "corral.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_ExecutionAPIMode(t *testing.T) {
	c, err := Load(writeCfg(t, `{
	  "component": {"name":"s","module":"github.com/me/s"},
	  "vendors": ["claude","codex"],
	  "execution": {
	    "claude": {"mode":"api","config":{"format":"openrouter","model":"anthropic/claude-sonnet-4","key_env":"OPENROUTER_API_KEY"}},
	    "codex":  {"mode":"subscription"}
	  },
	  "assets": []
	}`))
	if err != nil {
		t.Fatal(err)
	}
	got := c.Executions["claude"]
	if got.Mode != "api" || got.Config.Format != "openrouter" || got.Config.KeyEnv != "OPENROUTER_API_KEY" {
		t.Fatalf("claude execution = %+v", got)
	}
	if c.Executions["codex"].Mode != "subscription" {
		t.Fatalf("codex execution = %+v", c.Executions["codex"])
	}
}

func TestLoad_AbsentExecutionIsSubscription(t *testing.T) {
	c, err := Load(writeCfg(t, `{
	  "component": {"name":"s","module":"github.com/me/s"},
	  "vendors": ["claude"], "assets": []
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Executions["claude"]; ok {
		t.Error("absent execution must not create an entry (defaults to subscription downstream)")
	}
}

func TestLoad_RejectsLiteralKeyInKeyEnv(t *testing.T) {
	_, err := Load(writeCfg(t, `{
	  "component": {"name":"s","module":"github.com/me/s"},
	  "vendors": ["claude"],
	  "execution": {"claude": {"mode":"api","config":{"format":"openai","model":"gpt-4o","key_env":"sk-abc-123"}}},
	  "assets": []
	}`))
	if err == nil {
		t.Fatal("want error: key_env must be an env-var NAME, not a literal key")
	}
}

func TestLoad_RejectsBadModeAndFormat(t *testing.T) {
	if _, err := Load(writeCfg(t, `{
	  "component":{"name":"s","module":"github.com/me/s"},"vendors":["claude"],
	  "execution":{"claude":{"mode":"metered"}},"assets":[]}`)); err == nil {
		t.Error("want error on unknown mode")
	}
	if _, err := Load(writeCfg(t, `{
	  "component":{"name":"s","module":"github.com/me/s"},"vendors":["claude"],
	  "execution":{"claude":{"mode":"api","config":{"format":"cohere","model":"m","key_env":"K"}}},"assets":[]}`)); err == nil {
		t.Error("want error on unknown format")
	}
	if _, err := Load(writeCfg(t, `{
	  "component":{"name":"s","module":"github.com/me/s"},"vendors":["claude"],
	  "execution":{"claude":{"mode":"api","config":{"format":"openai","model":"","key_env":"K"}}},"assets":[]}`)); err == nil {
		t.Error("want error on missing model in api mode")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./aihost/ -run TestLoad`
Expected: FAIL — `Component.Executions` undefined; `execution` not parsed/validated.

- [ ] **Step 3: Write minimal implementation**

In `aihost/ir.go`, add the types (after `Component`):

```go
// Execution selects how a vendor's turns run: "subscription" (a coding-agent
// CLI, the default) or "api" (a metered API backend). Absent ⇒ subscription.
type Execution struct {
	Mode   string          `json:"mode"`
	Config ExecutionConfig `json:"config"`
}

// ExecutionConfig configures an "api" execution. KeyEnv is an environment
// variable NAME (never a literal key); the generated code resolves it at
// runtime via os.Getenv.
type ExecutionConfig struct {
	Format  string         `json:"format"`
	Model   string         `json:"model"`
	KeyEnv  string         `json:"key_env"`
	BaseURL string         `json:"base_url"`
	Routing map[string]any `json:"routing"`
}
```

Add `Executions map[string]Execution` to the `Component` struct. Add an env-name matcher near the top of `ir.go` (add `"regexp"` to imports):

```go
var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
```

In `Load`, extend the decode struct with `Execution map[string]Execution `json:"execution"``, then validate and attach before returning:

```go
	// decode struct gains:
	//   Execution map[string]Execution `json:"execution"`
	for vendor, ex := range s.Execution {
		if err := validateExecution(vendor, ex); err != nil {
			return nil, err
		}
	}
	// ... in the returned &Component{...}: add Executions: s.Execution,
```

Add the validator:

```go
func validateExecution(vendor string, ex Execution) error {
	switch ex.Mode {
	case "", "subscription":
		return nil
	case "api":
		// validated below
	default:
		return fmt.Errorf("aihost: vendor %q: unknown execution mode %q (want subscription|api)", vendor, ex.Mode)
	}
	switch ex.Config.Format {
	case "openrouter", "openai", "anthropic":
	default:
		return fmt.Errorf("aihost: vendor %q: unknown api format %q (want openrouter|openai|anthropic)", vendor, ex.Config.Format)
	}
	if ex.Config.Model == "" {
		return fmt.Errorf("aihost: vendor %q: api execution requires config.model", vendor)
	}
	if ex.Config.KeyEnv == "" {
		return fmt.Errorf("aihost: vendor %q: api execution requires config.key_env", vendor)
	}
	if !envNameRe.MatchString(ex.Config.KeyEnv) {
		return fmt.Errorf("aihost: vendor %q: config.key_env %q must be an environment-variable NAME, not a literal key", vendor, ex.Config.KeyEnv)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./aihost/ -run TestLoad`
Expected: PASS (new + existing `TestLoad_ParsesComponentAndAssets`).

- [ ] **Step 5: Commit**

```bash
git add aihost/ir.go aihost/ir_test.go
git commit -m "feat(aihost): execution IR — per-vendor api/subscription mode, key_env validation"
```

---

### Task 7: Generator — emit the `api` vs `subscription` `New()` per vendor

**Files:**
- Modify: `aihost/gen.go`
- Modify: `aihost/gen_test.go`

**Interfaces:**
- Consumes: `Component.Executions[vendor]` (Task 6); `corral.NewAgencyWithProvider` (Task 5); the `apiprovider`/`openrouter` `New(Config)` constructors (Tasks 2–4).
- Produces: for an `api`-mode vendor, `component.gen.go`'s `New()` constructs the configured backend and wraps it with `corral.NewAgencyWithProvider`; for a subscription vendor, the existing `corral.NewAgency(provider, ".")` is unchanged. The generator selects the package by format: `openrouter` → `github.com/inovacc/corral/openrouter`; `openai`/`anthropic` → `github.com/inovacc/corral/apiprovider` (with a `Format` field in the config literal).

- [ ] **Step 1: Write the failing test**

Append to `aihost/gen_test.go`:

```go
func componentGen(t *testing.T, files []GeneratedFile) string {
	t.Helper()
	for _, f := range files {
		if filepath.ToSlash(f.Path) == "claude/component.gen.go" {
			return string(f.Content)
		}
	}
	t.Fatal("claude/component.gen.go not found")
	return ""
}

func TestGenerate_APIExecutionEmitsBackend(t *testing.T) {
	RegisterVendor(func() Vendor { return fakeClaudeVendor{} })
	c := sampleComponent()
	c.Executions = map[string]Execution{
		"claude": {Mode: "api", Config: ExecutionConfig{
			Format: "openrouter", Model: "anthropic/claude-sonnet-4", KeyEnv: "OPENROUTER_API_KEY",
		}},
	}
	files, err := Generate(c, []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	src := componentGen(t, files)
	for _, want := range []string{
		`"github.com/inovacc/corral/openrouter"`,
		"openrouter.New(openrouter.Config{",
		`os.Getenv("OPENROUTER_API_KEY")`,
		`Model:   "anthropic/claude-sonnet-4"`,
		"corral.NewAgencyWithProvider(p,",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("component.gen.go missing %q\n---\n%s", want, src)
		}
	}
	if strings.Contains(src, "sk-") {
		t.Error("generated source must never contain a literal key")
	}
}

func TestGenerate_SubscriptionUnchanged(t *testing.T) {
	RegisterVendor(func() Vendor { return fakeClaudeVendor{} })
	files, err := Generate(sampleComponent(), []string{"claude"}) // no Executions
	if err != nil {
		t.Fatal(err)
	}
	src := componentGen(t, files)
	if !strings.Contains(src, `corral.NewAgency(provider, ".")`) {
		t.Errorf("subscription New() changed:\n%s", src)
	}
	if strings.Contains(src, "apiprovider") || strings.Contains(src, "openrouter.New") {
		t.Error("subscription component must not import an api backend")
	}
}

func TestGenerate_APIOpenAIUsesApiproviderPackage(t *testing.T) {
	RegisterVendor(func() Vendor { return fakeClaudeVendor{} })
	c := sampleComponent()
	c.Executions = map[string]Execution{
		"claude": {Mode: "api", Config: ExecutionConfig{
			Format: "openai", Model: "gpt-4o", KeyEnv: "OPENAI_API_KEY",
		}},
	}
	files, err := Generate(c, []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	src := componentGen(t, files)
	for _, want := range []string{
		`"github.com/inovacc/corral/apiprovider"`,
		"apiprovider.New(apiprovider.Config{",
		`Format:  "openai"`,
		`os.Getenv("OPENAI_API_KEY")`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("component.gen.go missing %q\n---\n%s", want, src)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./aihost/ -run TestGenerate_API`
Expected: FAIL — the generated `New()` is always the subscription form; no `ExtraImports`/`NewFunc` plumbing.

- [ ] **Step 3: Write minimal implementation**

In `aihost/gen.go`, replace `componentGenData` and its template, and build the `New()` body in `moduleWrapperFiles`.

Change the struct:

```go
type componentGenData struct {
	Vendor       string
	ExtraImports string // extra import lines (api backends); "" for subscription
	NewFunc      string // the full `func New(...) {...}` definition
}
```

Add a builder that turns an `Execution` into the import + function source (place near `moduleWrapperFiles`):

```go
// executionCodegen returns the extra import line(s) and the New() function body
// for a vendor's execution mode. A zero/subscription Execution yields the
// existing name-registry path; an api Execution constructs the configured
// backend and wraps it with corral.NewAgencyWithProvider.
func executionCodegen(module, vendor string, ex Execution) (extraImports, newFunc string) {
	if ex.Mode != "api" {
		return "", subscriptionNewFunc(vendor)
	}
	var pkgImport, ctorPkg, formatField string
	switch ex.Config.Format {
	case "openrouter":
		pkgImport = module // placeholder; overwritten below
	}
	// Select the backend package by format.
	switch ex.Config.Format {
	case "openrouter":
		pkgImport = "\t\"github.com/inovacc/corral/openrouter\"\n"
		ctorPkg = "openrouter"
	default: // openai | anthropic
		pkgImport = "\t\"github.com/inovacc/corral/apiprovider\"\n"
		ctorPkg = "apiprovider"
		formatField = fmt.Sprintf("\t\tFormat:  %q,\n", ex.Config.Format)
	}
	body := fmt.Sprintf(`// New returns a corral.Agency for the %q component, pinned at generation
// time to an %q API backend (model %q). The provider argument is ignored in
// api mode — the configured backend governs. The API key is read at runtime
// from the %s environment variable and is never baked into this source.
func New(provider string) (*corral.Agency, error) {
	_ = provider
	p, err := %s.New(%s.Config{
%s		Model:   %q,
		Key:     os.Getenv(%q),
		BaseURL: %q,
	})
	if err != nil {
		return nil, err
	}
	return corral.NewAgencyWithProvider(p, "."), nil
}
`, vendor, ex.Config.Format, ex.Config.Model, ex.Config.KeyEnv,
		ctorPkg, ctorPkg, formatField, ex.Config.Model, ex.Config.KeyEnv, ex.Config.BaseURL)
	return pkgImport, body
}

func subscriptionNewFunc(vendor string) string {
	return fmt.Sprintf(`// New returns a corral.Agency for the %q component, running against the
// process's current working directory ("."). corral decides which registered
// provider actually runs — pass the runtime provider name (e.g. "claude",
// "codex", "agy", "grok", "kimi").
func New(provider string) (*corral.Agency, error) {
	return corral.NewAgency(provider, ".")
}
`, vendor)
}
```

Delete the stray first `switch` with the `pkgImport = module` placeholder — it was scaffolding; the real selection is the second `switch`. Final `executionCodegen` keeps only the second switch.

In `moduleWrapperFiles`, compute the codegen and pass it in — replace the `componentGenTpl` render call:

```go
	extraImports, newFunc := executionCodegen(c.Module, vendor, c.Executions[vendor])
	compBuf, err := renderFormatted(componentGenTpl, componentGenData{
		Vendor: vendor, ExtraImports: extraImports, NewFunc: newFunc,
	})
```

Replace `componentGenTpl` so it interpolates the imports + function and drops the hardcoded `New()`:

```go
var componentGenTpl = template.Must(template.New("component.gen.go").Parse(
	`// Code generated by corral. DO NOT EDIT.

package ` + wrapperPackage + `

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
{{.ExtraImports}}
	"github.com/inovacc/corral"
)

//go:embed all:assets
var assetsFS embed.FS

{{.NewFunc}}
// Install writes the embedded plugin tree under target, returning the number
// of files written.
func Install(target string) (int, error) {
	n := 0
	err := fs.WalkDir(assetsFS, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel("assets", p)
		if err != nil {
			return err
		}
		data, err := assetsFS.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		tmp := dst + ".tmp"
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, dst); err != nil {
			return err
		}
		n++
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
`))
```

`go/format` (via `renderFormatted`) fixes import grouping/spacing, so the injected `{{.ExtraImports}}` line and `{{.NewFunc}}` block come out gofmt-clean.

> **Transitive dependency note:** an api-mode generated module imports `github.com/inovacc/corral/openrouter`, which pulls the go-sdk transitively. The generated `go.mod` declares only `require github.com/inovacc/corral`; `go mod tidy` in the generated module resolves the rest — the same tidy step already required to fetch corral itself. The generator test asserts on generated **source** (matching the existing `gen_test.go` approach), not on compiling the generated module in isolation.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./aihost/`
Expected: PASS — new api/subscription tests plus the existing `TestGenerate_*` (including `TestGenerate_GeneratedGoFilesParseCleanly`, which now parses the api-mode `New()` too). If `TestGenerate_ComponentGenEmbedsDotfiles` or the parse test fails, inspect the printed source and fix template whitespace.

- [ ] **Step 5: Commit**

```bash
git add aihost/gen.go aihost/gen_test.go
git commit -m "feat(aihost): generate api-mode New() constructing the configured backend"
```

---

### Task 8: Docs — record the two-dep thesis amendment + backlog

**Files:**
- Modify: `CLAUDE.md`
- Modify: `docs/BACKLOG.md`

**Interfaces:** none (documentation).

- [ ] **Step 1: Amend CLAUDE.md (dependency thesis + rev bump)**

Read `CLAUDE.md`. Find the section stating corral's one-dependency (`conpty`) thesis. Add a sentence recording the amendment, and bump the `<!-- rev:NNN -->` tag by exactly 1 (in the same edit). Example insertion (adapt to the file's actual wording):

> **Dependency thesis (amended 2026-07-05):** corral now carries **two** external dependencies — `conpty` (subscription CLI ConPTY driver) and `github.com/OpenRouterTeam/go-sdk v0.5.9` (the `api` execution mode's OpenRouter backend; Apache-2.0, beta, Go 1.25+). The hand-rolled `apiprovider` (OpenAI/Anthropic) backends add no further deps. Provider packages remain the only importers of a backend's SDK; the core stays SDK-free.

- [ ] **Step 2: Update docs/BACKLOG.md**

Read `docs/BACKLOG.md`. Mark item `#1` (component execution-mode selection) as delivered (API + subscription, per-vendor, OpenRouter/OpenAI/Anthropic), and add the deferred follow-ups as new backlog entries:
- OpenRouter native `response_format` structured output (v1 uses prompt-embedded schema).
- OpenRouter `routing` codegen in the generator (config field exists; not yet emitted).
- Streaming responses for api backends.
- API-backend `UsageReporter` for OpenRouter (subscription-usage / credits endpoint).

- [ ] **Step 3: Verify the whole suite is green**

Run:
```bash
go build ./...
go test ./...
```
Expected: build clean; all packages PASS (Docker-free).

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md docs/BACKLOG.md
git commit -m "docs: record two-dep thesis amendment + api-mode backlog follow-ups"
```

---

## Self-Review

**1. Spec coverage** (against `docs/superpowers/specs/2026-07-05-corral-api-execution-mode-design.md`):
- §1 api backends (openrouter/openai/anthropic, each a Provider) → Tasks 2, 3, 4. ✅
- §2 Run semantics (ComposePrompt, single user turn, ctx, wrapped errors) → Tasks 2–4. ✅
- §3 Structured output (openai response_format; anthropic tool-use; openrouter prompt-embed fallback) → Tasks 2, 3, 4 (openrouter native deferred per Global Constraints). ✅
- §4 UsageReporter from rate-limit headers → Tasks 2, 3 (openrouter usage deferred to BACKLOG — Task 8). ✅ (documented deferral)
- §5 Config schema (`execution`, `key_env` = NAME, absent ⇒ subscription, base_url/routing optional) → Task 6. Deviation: `execution` is a **top-level** map keyed by vendor rather than nested inside `vendors` — required because the existing `vendors` field is `[]string`; this keeps the change additive/backward-compatible (Global Constraints). ✅
- §6 Generator wiring (ir.go Execution IR + Load validate; generated New() branch) → Tasks 6, 7. Uses `corral.NewAgencyWithProvider` (Task 5) as the construction seam. ✅
- §7 Testing (httptest for hand-rolled; WithServerURL for SDK stub; generator golden) → Tasks 1–4, 7. ✅
- Non-goals (streaming, embeddings, retrofit, UI builder) → not built; logged in BACKLOG. ✅

**2. Placeholder scan:** No "TBD"/"handle edge cases"/prose-only steps — every code step carries complete code. The two SDK-shape confirmations (openrouter response accessor + `Send` return type) are explicit, test-guarded verification steps, not placeholders: the stub test fails to compile/pass until the real generated names are used.

**3. Type consistency:** `Config{Format,Model,BaseURL,Key}` (apiprovider) and `Config{Model,Key,BaseURL,Routing}` (openrouter) are used identically across constructors and generator codegen. `Execution{Mode, Config ExecutionConfig}` / `ExecutionConfig{Format,Model,KeyEnv,BaseURL,Routing}` match between Task 6 (IR) and Task 7 (generator reads `ex.Config.Format/Model/KeyEnv/BaseURL`). `NewAgencyWithProvider(p Provider, dir string) *Agency` is defined in Task 5 and called in Task 7's generated source. `rateLimitStatus(window, http.Header, limitHdr, remainingHdr)` signature is consistent across Tasks 1–3. Provider `Name()` values (`"openai"`, `"anthropic"`, `"openrouter"`) match the `RunResult.Provider` assertions in each test.

**Ambiguity resolved:** Task 1's compile-order wrinkle (the `New` switch references types defined in later tasks) is handled by folding minimal type declarations + not-implemented `Run` stubs into Task 1, which Tasks 2–3 replace — so every task compiles and its own tests pass in isolation.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-05-corral-api-execution-mode.md`. Two execution options:

1. **Subagent-Driven (recommended)** — fresh subagent per task, spec+quality review between tasks, broad review at the end.
2. **Inline Execution** — execute tasks in this session with checkpoints.

Which approach?
