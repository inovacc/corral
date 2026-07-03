# KB: x.ai Grok CLI — usage/quota reverse-engineering

<!-- rev:001 -->

> Goal: replicate Grok CLI's own usage/quota ("/usage") reporting as a corral `UsageReporter`.
> Legend: **[M]** = measured (found in the shipped PE / bundled docs) · **[I]** = inferred.

## Product / binary

- **Product:** x.ai **Grok** CLI — self-identifies as "Grok Build TUI". clap-based Rust CLI. **[M]**
- **Client version:** header `x-grok-client-version: 0.2.82`. **[M]**
- **Binary:** `C:/Users/dyamm/.grok/bin/grok.exe` and `.../agent.exe` are **byte-identical** (md5 `9da49238b3aec8ad02275cd449de951a`), 134,171,976 bytes (~128 MiB), PE x86_64 (`x86_64-pc-windows-msvc`, `release-dist`). **[M]**
- **Source lineage:** monorepo `xai/xai`, primary crate `xai-grok-shell` (+ `xai-grok-pager`, `xai-grok-tools`, `xai-grok-telemetry`, `xai-data-collector`). Deliberately mirrors Claude Code conventions (`.claude.json`, `--allow`/`--deny` == `--allowedTools`, `managed-settings.json`). **[M]**
- **Extraction:** `unravel garble strings grok.exe --min-len 6 --json` → 324,474 strings; mined on disk (never pasted). Config docs also ship as readable markdown under `~/.grok/docs/user-guide/`. **[M]**

## HEADLINE — Grok DOES have a dedicated remote usage/billing REST call

Unlike Qwen/gemini-cli, the `/usage` slash command ("View credit usage or manage billing") is backed by a **real remote fetch** in `crates/codegen/xai-grok-shell/src/extensions/billing.rs`. Two REST calls: **[M]**

- **`GET /billing?format=credits`** → "credits config" / billing data. Log line: `billing: fetched credits config`. **[M]**
- **`GET /auto-topup-rule`** → auto top-up rule (min/max monthly top-up, enabled flag). **[M]**

`/usage` also has a browser side: `/usage show | manage` opens `https://grok.com/?_s=usage` for billing management; the REST call above powers the in-TUI `show`. **[M]**

There is **also** a gateway-RPC path: methods `x.ai/billing` and `x.ai/auto-topup-rule` exist in the `x.ai/*` JSON-RPC namespace carried over the production gateway WebSocket `wss://grok.com/ws/gw/`. So billing is reachable either as REST (cli-chat-proxy) or as a gateway verb. For corral the REST path is far simpler. **[M]** (that both surfaces exist) / **[I]** (that they return equivalent data)

## The usage/billing endpoint

| Field | Value | Ev |
|---|---|---|
| Method + path | `GET /billing?format=credits` (and `GET /auto-topup-rule`) | **[M]** |
| Base URL | cli-chat-proxy base = `https://cli-chat-proxy.grok.com/v1` (env `GROK_CLI_CHAT_PROXY_BASE_URL`) → full: `GET https://cli-chat-proxy.grok.com/v1/billing?format=credits` | base **[M]**, that billing rides this base **[I]** |
| Why that base | billing.rs sends the exact cli-chat-proxy client header set (`X-XAI-Token-Auth: xai-grok-cli` + `x-grok-client-version`), the same client used for `/chat/completions` and `/embeddings`; path is host-relative | **[I]** |
| Method = GET | read-only fetch with a `format=` query param; no body seen | **[I]** |
| Auth-required error | `Authentication required to fetch billing data` / `Billing data requires auth with grok.com. Run \`grok login\`` | **[M]** |
| Error strings | `billing: upstream error`, `Billing service error:`, `Failed to fetch billing data:` | **[M]** |

Other hosts in the binary (context): public xAI API `https://api.x.ai/v1` (`GROK_XAI_API_BASE_URL`); models base `GROK_MODELS_BASE_URL` (BYOK, `{base}/models`); telemetry events `https://grok.com/_data/v1/events`; code-agent WS `wss://code.grok.com/ws/code-agent`; computer-hub `wss://computer-hub.grok.com/v1/tools`; assets `https://assets.grok.com`. **[M]**

## Auth source + request headers (measured)

billing.rs request headers: **[M]**

- `Authorization: Bearer <access_token>`
- `X-XAI-Token-Auth: xai-grok-cli`  ← tells the xAI auth middleware this is a Grok-CLI session token (not a raw API key)
- `x-userid: <user_id>`
- `x-grok-client-version: 0.2.82`

**Credential file corral must read: `~/.grok/auth.json`.** Stored by `grok login` (OAuth via `auth.x.ai`, or device-code). Grok auto-refreshes in the background; creds without server expiry fall back to a 30-day lifetime. **[M]**

`auth.json` / token-exchange response schema (measured field tokens `…AuthResponse`): `access_token`, `refresh_token`, `expires_at`, `token_type`, `scope`, `user_id`, `email`, `client_id`, `issuer`, `principal_type`, `principal_id`, `principal_display_name`. So corral reads **`access_token`** (→ Bearer) and **`user_id`** (→ `x-userid`). **[M]**

Auth flow details: OAuth2 default at `https://auth.x.ai` (discovery `{issuer}/.well-known/openid-configuration`); scopes `openid profile email offline_access api:access`; device-code path `POST /oauth2/device/code` (headers `x-grok-client-version`, `x-grok-client-surface`, `referrer=grok-build`); silent refresh via `refresh_token`; on `401` Grok refreshes and retries. **Fallback creds:** `XAI_API_KEY` env (BYOK) when no session token. **Resolution order:** explicit external provider > `~/.grok/auth.json` session token > `XAI_API_KEY`. **[M]**

## Response fields that carry consumption / limit / reset

Measured field tokens adjacent to `billing.rs` (`…AuthResponse`/credits-config struct): **[M]**

- **Tier:** `subscription_tier` / `subscriptionTier` (+ `subscription_tier_display`). Also surfaced on the auth/userinfo enrichment (`organizationName`, `organizationRole`, `teamBlockedReason`, `userBlocked`, `codingDataRetentionOptOut`).
- **Window:** `billingCycle` + `periodEnd` (billing-cycle end date).
- **Consumption:** `includedUsed` (of the plan's included allowance), `totalUsed`.
- **On-demand:** `on_demand_enabled` (pay-as-you-go toggle).
- **History:** `history` (per-cycle usage history array).

`/usage`-rendered display strings derived from the above (measured): `Credits left:`, `Pay-as-you-go: $<used> of $<limit>`, `Pay-as-you-go limit left:`, `Min auto topup`, `Max monthly topup`, `Auto topup: disabled`. **[M]**

> Note: the API returns raw counters (`includedUsed`, `totalUsed`, on-demand limit, `periodEnd`) — **no pre-computed `remaining` / `percent` field**; the CLI computes "Credits left" / "limit left" itself. **[M]**

### "Approaching the safe threshold" — how corral should signal it

No server-provided percentage. Compute client-side from the credits response: **[I]**
- `included_left = includedAllowance − includedUsed`; when `on_demand_enabled == false` and `included_left → 0` you are at the hard wall.
- With pay-as-you-go on, watch `payg_limit − payg_used` ("Pay-as-you-go limit left").
- Alert at e.g. 80/90% of the cycle allowance, and reset the counter at `periodEnd`.

Hard-stop / exhaustion signals actually emitted (measured strings): **[M]**
- `You've hit your spending cap.` → `Increase limit` / `Raise your pay-as-you-go spending cap` → `https://grok.com/supergrok?referrer=grok-build` (`credit-limit-upsell`).
- `A subscription is required.` → `https://grok.com/supergrok?referrer=grok-build`.
- `Upgrade to a higher tier for more credits`, `Enable pay-as-you-go credits for on-demand usage`, `You can continue by enabling pay-as-you-go usage` / `…by increasing your spending limit`.
- Time-based entitlement limits are surfaced inline on the **chat stream** ("try later / upgrade"), plus generic HTTP `Retry-After` handling — not a dedicated quota field. **[M]**

## ACP support

**Yes.** The entire Grok session layer is built on the Zed **`agent-client-protocol` v0.6.0** crate (`crates/codegen/xai-grok-shell/src/session/acp_session.rs`, `acp_session_impl/*`). **[M]**

- `grok agent stdio` — "Run the agent over stdio" = the ACP/JSON-RPC-over-stdio transport (the corral-relevant one). **[M]**
- `grok agent headless` — over the grok.com WebSocket relay; `grok agent serve` — WebSocket server; `grok agent leader` — shared leader process. **[M]**
- Headless single-turn (Claude-Code-style): `grok -p` / `--prompt-file` / `--prompt-json` (content blocks), `--output-format`, `--best-of-n`, `--check`. **[M]**

## corral UsageReporter plan (Grok provider)

Grok, unlike Qwen, exposes a queryable usage endpoint — implement `Usage()` as a **direct remote GET**:

1. **Load creds.** Read `~/.grok/auth.json`; take `access_token` + `user_id`. If absent, fall back to `XAI_API_KEY` env (BYOK). If `expires_at` is past, corral should treat as stale (Grok itself silent-refreshes via `refresh_token` against `auth.x.ai`; corral can either reuse the token until 401 or re-run `grok login`). **[M]**
2. **Resolve base URL.** `GROK_CLI_CHAT_PROXY_BASE_URL` if set, else `https://cli-chat-proxy.grok.com/v1`. **[M]**
3. **Fetch usage.** `GET {base}/billing?format=credits` with headers `Authorization: Bearer <access_token>`, `X-XAI-Token-Auth: xai-grok-cli`, `x-userid: <user_id>`, `x-grok-client-version: 0.2.82`. Optionally also `GET {base}/auto-topup-rule` for the top-up rule. **[M for the request shape / I for base host]**
4. **Parse.** Read `subscription_tier`, `includedUsed`, `totalUsed`, `on_demand_enabled`, `billingCycle.periodEnd`, and (if present) the pay-as-you-go used/limit. **[M for field names]**
5. **Derive remaining + reset.** `remaining = includedAllowance − includedUsed`; `reset = periodEnd`; if `on_demand_enabled`, add pay-as-you-go headroom. **[I]**
6. **Threshold alert.** Warn at 80/90% of allowance, or when `on_demand_enabled == false` and included credits are exhausted; treat a `You've hit your spending cap` / `A subscription is required` / `401` as the hard-stop states. **[I]**
7. **(Optional) gateway path.** For parity with a live session, the same data is available as the `x.ai/billing` verb over `wss://grok.com/ws/gw/`, but REST is the recommended corral surface. **[I]**

## Confidence

**HIGH** on: the endpoint path (`GET /billing?format=credits`) + `/auto-topup-rule`, the four request headers, the credential file (`~/.grok/auth.json`) and its `access_token`/`user_id` fields, the response field names (`subscription_tier`, `includedUsed`, `totalUsed`, `on_demand_enabled`, `billingCycle`/`periodEnd`), the OAuth/`auth.x.ai` flow, and ACP (`agent-client-protocol` v0.6.0, `grok agent stdio`) — all directly measured in the v0.2.82 PE and bundled docs.
**MEDIUM (inferred)** on: that `/billing` is served off the cli-chat-proxy base (strongly implied by the shared header set with `/embeddings`, but not a literal concatenated URL in the strings), the GET method, exact JSON nesting of `billingCycle`, and the client-side threshold heuristic (Grok returns raw counters, not a percent).
