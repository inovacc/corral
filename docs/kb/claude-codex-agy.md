# Subscription Usage Calls — Claude Code, Codex, Antigravity

<!-- rev:001 -->

Knowledge base for the three **usage / rate-limit** calls corral makes to power
`corral.UsageReporter` monitoring. Every fact below is transcribed from the
existing provider source — nothing here is reverse-engineered afresh.

Source files:

| Provider | Code | Package doc |
|----------|------|-------------|
| Claude Code | `claude/usage.go` | `claude/claude.go` |
| Codex | `codex/api.go` | `codex/codex.go` |
| Antigravity | `agy/usage.go` | `agy/doc.go` |

---

## Shared target type — `corral.LimitStatus`

Defined in `monitor.go` (package `corral`). Every provider normalizes its native
response into this vendor-neutral snapshot.

```go
type LimitWindow struct {
    Name        string    // "5h" | "weekly" | provider-specific label
    UsedPercent float64   // 0..100
    ResetsAt    time.Time // window rollover (zero if unknown)
}

type LimitStatus struct {
    Plan    string        // subscription plan label
    Windows []LimitWindow // per-window usage
    Source  string        // provenance tag (endpoint / file)
}
```

**Approaching-threshold logic (identical for all three providers):**

- `LimitStatus.Worst()` = the **maximum `UsedPercent`** across all windows — the binding constraint.
- `LimitStatus.OK(threshold)` = `Worst() < threshold` (a `threshold <= 0` disables the gate).
- `LimitStatus.Exhausted()` = `Worst() >= 100`.
- The Agency gate `checkLimit` (in `monitor.go`) withholds a turn and returns `ErrRateLimited` once `Worst()` reaches `LimitThreshold`. Default = `DefaultLimitThreshold = 98` (see `agency.go`). A missing snapshot, an unsupported provider, or an error is **never** blocking — only a positive over-limit signal stops work.

So for every provider the single field that signals "approaching threshold" is the
per-window **`UsedPercent`** that lands in `LimitWindow`; the sections below name
the native field each one is derived from.

---

## 1. Claude Code — `claude/usage.go`

### Endpoint
`GET https://api.anthropic.com/api/oauth/usage`

- Base host `usageBaseURL = "https://api.anthropic.com"` (test-overridable).
- The same call the CLI's `/usage` and `/status` slash commands make.

### Request headers
| Header | Value |
|--------|-------|
| `Authorization` | `Bearer <accessToken>` |
| `anthropic-beta` | `oauth-2025-04-20` (`oauthBeta`) |
| `Content-Type` | `application/json` |
| `User-Agent` | `claude-code/2.0.30` (`userAgent`, pinned) |

Client timeout 10s in `ReadUsage`; the `Provider.Usage()` wrapper bounds it to 12s.

### Auth source
`~/.claude/.credentials.json` (or `$CLAUDE_CONFIG_DIR/.credentials.json`).
On macOS, falls back to the login keychain item **service `Claude Code-credentials`**
via `/usr/bin/security find-generic-password`.

On-disk shape — wrapper `{"claudeAiOauth": {...}}` decoded into `oauthCreds`:

| JSON field | Go field | Use |
|------------|----------|-----|
| `accessToken` | `AccessToken` | bearer |
| `refreshToken` | `RefreshToken` | (not used here) |
| `expiresAt` | `ExpiresAt` | epoch **milliseconds**; expiry gate |
| `scopes` | `Scopes` | — |
| `subscriptionType` | `SubscriptionType` (`*string`) | → `LimitStatus.Plan` |

**Expiry:** if `expiresAt > 0 && now >= expiresAt` → returns an error telling the
user to run `claude` to refresh. It deliberately **does not** refresh out-of-band
(that would rotate and corrupt the CLI's one-time refresh token).

### Response → `LimitStatus` mapping
Body `apiUsage` — three optional windows, each `apiWindow{ utilization *float64 (0..100), resets_at *string (ISO-8601) }`:

| Response field | `LimitWindow.Name` | `UsedPercent` | `ResetsAt` |
|----------------|--------------------|---------------|------------|
| `five_hour` | `"5h"` | `utilization` | parse `resets_at` (RFC3339) |
| `seven_day` | `"weekly"` | `utilization` | parse `resets_at` |
| `seven_day_opus` | `"weekly-opus"` | `utilization` | parse `resets_at` |

- `LimitStatus.Source = "api/oauth/usage"`; `Plan = *subscriptionType` when set.
- A window is skipped when the object or its `utilization` is nil.
- Returns `(nil, nil)` when no window survived (treated as "not monitored", non-blocking).

### Approaching-threshold field
**`utilization`** (per window) → `UsedPercent`. Worst of `5h` / `weekly` / `weekly-opus` vs. threshold 98.

---

## 2. Codex — `codex/api.go`

### Endpoint
`GET {chatgpt_base_url}/wham/usage`

- Base `codexUsageBaseURL`, default `https://chatgpt.com/backend-api` → effective URL `https://chatgpt.com/backend-api/wham/usage`.
- Overridable via **`CODEX_USAGE_BASE_URL`** (parity with a custom `chatgpt_base_url`).
- A dedicated account endpoint (codex-rs `BackendClient::get_rate_limits_with_reset_credits`) — **not** the per-turn `/responses` headers. Same data the CLI `/usage` and `/status` show.

### Request headers
| Header | Value |
|--------|-------|
| `Authorization` | `Bearer <bearer>` |
| `User-Agent` | `codex_cli_rs` (`codexUserAgent`, mirrors `DEFAULT_ORIGINATOR`) |
| `ChatGPT-Account-Id` | `<account_id>` — only when present |

Client timeout 12s; `Provider.Usage()` wrapper bounds to 13s.

### Auth source
`$CODEX_HOME/auth.json`, default `~/.codex/auth.json`. Decoded into `authFile`:

| JSON field | Go field |
|------------|----------|
| `OPENAI_API_KEY` | `OpenAIAPIKey` |
| `tokens.access_token` | `Tokens.AccessToken` |
| `tokens.refresh_token` | `Tokens.RefreshToken` |
| `tokens.id_token` | `Tokens.IDToken` |
| `tokens.account_id` | `Tokens.AccountID` |

**`bearer()`** = `tokens.access_token` when non-empty, else `OPENAI_API_KEY`.
Load fails ("run `codex login`") when both are empty.

### Response → `LimitStatus` mapping
Body `usageResponse` (the `RateLimitStatusPayload`, flattened) — only the surfaced
fields are decoded:

- `plan_type` → `LimitStatus.Plan`
- `rate_limit { primary_window, secondary_window }`, each `apiRateWindow`:
  - `used_percent` (int)
  - `limit_window_seconds` (int)
  - `reset_after_seconds` (int)
  - `reset_at` (int64, **unix epoch seconds**)

| Response window | `LimitWindow.Name` | `UsedPercent` | `ResetsAt` |
|-----------------|--------------------|---------------|------------|
| `rate_limit.primary_window` | `"5h"` | `float64(used_percent)` | `time.Unix(reset_at)` if `reset_at>0`, else `now + reset_after_seconds` |
| `rate_limit.secondary_window` | `"weekly"` | `float64(used_percent)` | same rule |

- `LimitStatus.Source = "wham/usage"`.
- Returns `(nil, nil)` when no window survived.

### Fallback path (`codex/codex.go` `Provider.Usage`)
If the live `FetchUsage` call fails or returns nil, it falls back to `ReadUsage()` —
the rate-limit headers the last `/responses` turn persisted under `~/.codex/sessions`
— via `.Status()`, so monitoring still has data when air-gapped. Then re-tries
`FetchUsage` as a last resort.

### Approaching-threshold field
**`used_percent`** (per window) → `UsedPercent`. Worst of `5h` / `weekly` vs. threshold 98.

---

## 3. Antigravity — `agy/usage.go`

### Endpoint
`POST {base}/v1internal:retrieveUserQuotaSummary` (Google Code Assist backend, JSON-transcoded v1internal RPC).

- Base `quotaBaseURL`, default `https://daily-cloudcode-pa.googleapis.com` (the **DAILY** channel host captured live; the prod host `cloudcode-pa.googleapis.com` returns 403 for this flow).
- Overridable via **`ANTIGRAVITY_BASE_URL`**.
- Body: `{"project":"<cloudaicompanionProject>"}`.
- A prerequisite call `POST {base}/v1internal:loadCodeAssist` (body `{"metadata":{"pluginType":"GEMINI"}}`) resolves `cloudaicompanionProject`, unless `GOOGLE_CLOUD_PROJECT` overrides it.

### Request headers
| Header | Value |
|--------|-------|
| `Authorization` | `Bearer <access_token>` |
| `Content-Type` | `application/json` |
| `User-Agent` | `antigravity/cli/1.0.11 windows/amd64` (`antigravityUA`, the endpoint gates on it) |

Client timeout 12s per call.

### Auth source
`~/.gemini/oauth_creds.json` (or `$GEMINI_DIR/oauth_creds.json`) — Antigravity shares
gemini-cli's Google OAuth credential file. Decoded into `geminiCreds`:

| JSON field | Go field | Use |
|------------|----------|-----|
| `access_token` | `AccessToken` | bearer |
| `refresh_token` | `RefreshToken` | — |
| `token_type` | `TokenType` | — |
| `expiry_date` | `ExpiryDate` | epoch **milliseconds**; expiry gate |

**Expiry:** if `expiry_date > 0 && now >= expiry_date` → error ("run `agy` to refresh").

**Entitlement caveat (load-bearing):** the `~/.gemini` token is gemini-cli's OAuth
client and **lacks the Antigravity-Pro entitlement** — the same request returns
`403 PERMISSION_DENIED` with it. `agy` mints its own entitled token through a
separate auth flow that is **not persisted to any readable file**, so on `403` the
code returns a clear, explicit error rather than pretending. In practice this call
is not standalone-reproducible from the shared creds file.

### Response → `LimitStatus` mapping
The 200 body is gzipped and its exact field nesting was not decodable from the
capture, so it is decoded into `any` and walked **tolerantly** by `collectBuckets`:
any "bucket-shaped" object (a name **plus** a usable percentage) becomes a `LimitWindow`.

- `LimitWindow.Name` = first of `displayName` / `name` / `quotaId`.
- `LimitWindow.UsedPercent` (`usedPercent`, clamped 0..100), tried in the object and a nested `bucketInfo`, in priority order:
  1. `remainingFraction` (0..1) → `(1 - remainingFraction) * 100`
  2. `consumed` / `limit` → `consumed / limit * 100`
  3. `remainingAmount` / `limit` → `(limit - remainingAmount) / limit * 100`
- `LimitWindow.ResetsAt` = parse `resetTime` / `resetAt` (RFC3339) when present.
- `LimitStatus.Plan = "antigravity"`, `Source = "retrieveUserQuotaSummary"`.
- One `LimitWindow` per discovered bucket; returns `(nil, nil)` when none found.

### Approaching-threshold field
The derived **`UsedPercent`** — primarily from **`remainingFraction`** (`1 - fraction`),
else the **`consumed` / `limit`** (or `remainingAmount` / `limit`) pair. Worst bucket
vs. threshold 98.

---

## Cross-provider summary

| | Claude Code | Codex | Antigravity |
|---|---|---|---|
| Method + path | `GET /api/oauth/usage` | `GET /wham/usage` | `POST /v1internal:retrieveUserQuotaSummary` |
| Host | `api.anthropic.com` | `{chatgpt_base_url}` (`chatgpt.com/backend-api`) | `daily-cloudcode-pa.googleapis.com` |
| Base override env | — (test only) | `CODEX_USAGE_BASE_URL` | `ANTIGRAVITY_BASE_URL` |
| Auth file | `~/.claude/.credentials.json` (macOS keychain fallback) | `~/.codex/auth.json` | `~/.gemini/oauth_creds.json` |
| Bearer field | `claudeAiOauth.accessToken` | `tokens.access_token` else `OPENAI_API_KEY` | `access_token` |
| Extra auth header | `anthropic-beta` | `ChatGPT-Account-Id` | — (project in body) |
| User-Agent | `claude-code/2.0.30` | `codex_cli_rs` | `antigravity/cli/1.0.11 windows/amd64` |
| `Plan` from | `subscriptionType` | `plan_type` | literal `"antigravity"` |
| `Source` tag | `api/oauth/usage` | `wham/usage` | `retrieveUserQuotaSummary` |
| Windows | `5h`, `weekly`, `weekly-opus` | `5h`, `weekly` | one per quota bucket |
| Used-% field | `utilization` | `used_percent` | derived (`remainingFraction` / `consumed`/`limit`) |
| Reset field | `resets_at` (ISO-8601) | `reset_at` (epoch s) / `reset_after_seconds` | `resetTime` / `resetAt` (RFC3339) |
| Expiry gate | `expiresAt` (ms) | none (token/key) | `expiry_date` (ms) |
| Notable | no out-of-band refresh | header-file fallback via `ReadUsage()` | 403 without Antigravity-Pro entitlement |

All three feed the identical Agency gate: `Worst()` (max `UsedPercent`) `>=` `LimitThreshold`
(default **98**) → `ErrRateLimited`; absence of data never blocks.
