# Provider Usage Calls — Cross-Provider Reference

<!-- rev:002 -->

Index + synthesis for the five-provider usage/quota KB in this directory. Each
provider page documents the one call corral makes to feed a
`corral.UsageReporter`; this README compares them, maps each onto the
vendor-neutral `corral.LimitStatus`, and specifies the background Monitor that
polls them and fires threshold alerts.

Source pages: [`claude-codex-agy.md`](./claude-codex-agy.md) (Claude Code,
Codex, Antigravity), [`grok.md`](./grok.md), and [`kimi.md`](./kimi.md).

---

## 1. Cross-provider comparison

| Provider | Binary / bundle | Usage endpoint | Auth source (cred file) | Key response fields (remaining / limit / reset) | Threshold signal | ACP |
|---|---|---|---|---|---|---|
| **Claude Code** | Claude Code CLI · Go impl `claude/usage.go` · UA `claude-code/2.0.30` | `GET api.anthropic.com/api/oauth/usage` | `~/.claude/.credentials.json` (macOS keychain fallback `Claude Code-credentials`) | per-window `utilization` (0..100); `resets_at` (ISO-8601); windows `five_hour`/`seven_day`/`seven_day_opus` | server `utilization` → `UsedPercent`; Worst ≥ 98 | n/d (KB = usage call only) |
| **Codex** | Codex CLI (`codex_cli_rs`, Rust) · Go impl `codex/api.go` | `GET {chatgpt_base_url}/wham/usage` (dflt `chatgpt.com/backend-api`) | `~/.codex/auth.json` (`$CODEX_HOME`) | `used_percent` (int); `reset_at` (epoch s) / `reset_after_seconds` / `limit_window_seconds`; windows `primary`/`secondary` | server `used_percent` → `UsedPercent`; Worst ≥ 98 | n/d |
| **Antigravity** | antigravity CLI 1.0.11 (Google Code Assist) · Go impl `agy/usage.go` | `POST {base}/v1internal:retrieveUserQuotaSummary` (`daily-cloudcode-pa.googleapis.com`) | `~/.gemini/oauth_creds.json` (shared w/ gemini-cli; **lacks** Antigravity-Pro entitlement → 403) | derived from `remainingFraction` **or** `consumed`/`limit` **or** `remainingAmount`/`limit`; `resetTime`/`resetAt` | derived `UsedPercent`; Worst ≥ 98 | n/d |
| **Grok** | `grok.exe`/`agent.exe` (byte-identical) · clap Rust PE ~128 MiB · v0.2.82 | `GET cli-chat-proxy.grok.com/v1/billing?format=credits` (+ `/auto-topup-rule`) | `~/.grok/auth.json` (`access_token`+`user_id`; else `XAI_API_KEY` BYOK) | raw counters only: `includedUsed`, `totalUsed`, `on_demand_enabled`, `billingCycle.periodEnd`, `subscription_tier` — **no** `remaining`/`percent` | client-computed; alert 80/90% of allowance; hard stop = "spending cap"/"subscription required"/401 | **Yes** — ACP v0.6.0, `grok agent stdio` |
| **Kimi** | `kimi.exe` · Bun-compiled PE (Node v24.15.0) ~120 MiB · v0.22.2 | `GET api.kimi.com/coding/v1/usages` (managed plan only; BYO key 404s) | `~/.kimi-code/credentials/` (key `oauth/kimi-code-env-<sha256>`, field `access_token`) | `limit`, `used` (or `remaining`→`limit−remaining`); `window{duration,timeUnit}`; reset `reset_at`/`resetAt`/`reset_time`/`resetTime` or `reset_in`/`ttl`/`window` | server `used`+`limit` → ratio; `ratioSeverity` ≥ 0.85 danger, ≥ 0.50 warn | **Yes** — `kimi acp` (stdio) |

ACP column for Claude/Codex/Antigravity is *n/d* because `claude-codex-agy.md`
scopes strictly to the usage call and does not document those CLIs' ACP surface.

---

## 2. Unified `LimitStatus` mapping

Every provider normalizes into the single vendor-neutral snapshot from
`monitor.go` (package `corral`):

```go
type LimitWindow struct {
    Name        string    // "5h" | "weekly" | provider label
    UsedPercent float64   // 0..100 — the one binding number
    ResetsAt    time.Time // window rollover (zero if unknown)
}
type LimitStatus struct {
    Plan    string        // subscription plan label
    Windows []LimitWindow // per-window usage
    Source  string        // provenance (endpoint / file)
}
```

**The normalized field that drives the "approaching threshold" alert is always
`LimitWindow.UsedPercent`, reduced across windows by `LimitStatus.Worst()` (the
max).** Reset windows are informational (display / "resets in X"); the binding
constraint is the worst per-window ratio. Providers split into two families by
*where that percent comes from*:

**A. Server already returns a percent** (map directly):

| Provider | Native field → `UsedPercent` | `Name` | `ResetsAt` | `Plan` / `Source` |
|---|---|---|---|---|
| Claude | `utilization` (0..100) | `5h`,`weekly`,`weekly-opus` | `resets_at` (RFC3339) | `subscriptionType` / `api/oauth/usage` |
| Codex | `float64(used_percent)` | `5h`,`weekly` | `time.Unix(reset_at)` else `now+reset_after_seconds` | `plan_type` / `wham/usage` |
| Antigravity | `1−remainingFraction`×100, else `consumed/limit`×100 | one per quota bucket (`displayName`/`name`/`quotaId`) | `resetTime`/`resetAt` | `"antigravity"` / `retrieveUserQuotaSummary` |

**B. Server returns raw counters** (corral computes the percent):

| Provider | Compute `UsedPercent` | `ResetsAt` | Notes |
|---|---|---|---|
| Kimi | `used / limit × 100` (`used = used ?? limit−remaining`) per row | `reset_*` abs, else `reset_in`/`ttl`/`window` sec | matches native `ratioSeverity` (0.85/0.50) |
| Grok | `includedUsed / includedAllowance × 100` (+ pay-as-you-go `used/limit` when `on_demand_enabled`) | `billingCycle.periodEnd` | no server percent; allowance is the denominator |

Family A snapshots are authoritative; Family B synthesizes `UsedPercent` so the
*same* `Worst()`-vs-threshold gate works uniformly. Kimi's server-side
`used`+`limit` makes it a *real* proactive percent; Grok has real counters but
no precomputed ratio.

---

## 3. corral Monitor + alerting design

Each provider implements one interface; a shared Monitor polls them and fires
alerts. Two thresholds exist and must not be conflated: a **soft alert**
threshold (`AlertThreshold`, e.g. 80) that only *warns*, and the existing
**hard gate** (`LimitThreshold`, default `DefaultLimitThreshold = 98` in
`agency.go`) that the Agency `checkLimit` uses to withhold a turn and return
`ErrRateLimited`.

```go
type UsageReporter interface {
    Provider() string
    Usage(ctx context.Context) (*corral.LimitStatus, error) // nil,nil = "not monitored"
}

type MonitorConfig struct {
    Interval       time.Duration // poll cadence, e.g. 60s
    AlertThreshold float64       // soft warn, e.g. 80
    LimitThreshold float64       // hard gate, default 98
    ClearMargin    float64       // hysteresis, e.g. 5 (clear at Alert−margin)
}
```

**Poll loop (per reporter, on `Interval` ticker):**

1. `ctx, cancel := context.WithTimeout(ctx, providerTimeout)` — bound each call
   (Claude 12s, Codex 13s, Antigravity/Grok/Kimi ~8–12s).
2. `status, err := r.Usage(ctx)`. On `err` **or** `status == nil`: keep the last
   good snapshot, log, and **do not block** — a missing/unsupported/errored
   snapshot is never a positive over-limit signal (mirrors `checkLimit`).
3. `worst := status.Worst()`; `headroom := 100 - worst`.
4. **Alert edge:** if `worst >= AlertThreshold` and the provider was not already
   alerting → emit one alert `{provider, plan, worstWindow.Name, worst%,
   resetsAt}`. Clear the alerting flag only when `worst < AlertThreshold −
   ClearMargin` (hysteresis stops flapping around the boundary).
5. **Hard gate:** if `worst >= LimitThreshold` → mark the provider rate-limited;
   this is the same signal `checkLimit` reads to return `ErrRateLimited`.
6. Cache `status` + `worst`'s `ResetsAt` for display ("resets in X") and cheap
   reads by the Agency between polls.

**Cadence by family.** Remote-GET providers (Claude, Codex, Antigravity, Grok,
Kimi) are polled on the ticker with per-call timeout + error backoff. Codex
additionally falls back to its persisted `/responses` rate-limit headers
(`ReadUsage()`) when the live fetch fails, so its snapshot survives an air-gap.

Alert transport is orthogonal (log line, notifier, TUI banner); the Monitor's
job is only headroom computation + edge detection at `AlertThreshold`, with the
`LimitThreshold` gate reserved for actually stopping work.

---

## 4. Implementation status

| Provider | `UsageReporter` | Where / plan |
|---|---|---|
| Claude Code | **Done** | `claude/usage.go` — direct GET, maps `utilization` |
| Codex | **Done** | `codex/api.go` — GET `wham/usage` + `ReadUsage()` header fallback |
| Antigravity | **Done** | `agy/usage.go` — POST quota RPC, tolerant bucket walk |
| **Grok** | **TODO** | plan in `grok.md` §"corral UsageReporter plan" — direct GET `/billing?format=credits`, compute percent from `includedUsed`/allowance |
| **Kimi** | **TODO** | plan in `kimi.md` §"corral UsageReporter plan" — single GET `/usages`, `used/limit` ratio (mirror `ratioSeverity`) |

Grok/Kimi have measured, HIGH-confidence KB plans ready to implement;
only their corral `Usage()` code is outstanding.
