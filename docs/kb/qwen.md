# KB: Alibaba Qwen Code CLI — usage/quota reverse-engineering

<!-- rev:001 -->

> Goal: replicate Qwen Code's own usage/quota reporting as a corral `UsageReporter`.
> Legend: **[M]** = measured (found in the shipped bundle) · **[I]** = inferred.

## Product / binary

- **Product:** Alibaba **Qwen Code** CLI (a fork of Google `gemini-cli`). **[M]**
- **Package / version:** `@qwen-code/qwen-code` **v0.19.6**. **[M]**
- **Binary type:** readable Node.js ESM bundle — esbuild code-split into `chunks/*.js` (600+ chunks) under
  `C:/Users/dyamm/scoop/persist/nodejs/bin/node_modules/@qwen-code/qwen-code`. Not a compiled PE; no unravel needed. **[M]**
- **Lineage:** carries both the Alibaba **DashScope** stack and the Google **gemini-cli** stack
  (`generativelanguage.googleapis.com`, `oauth2.googleapis.com`). **[M]**

## HEADLINE FINDING — there is no dedicated remote "/usage" or "/quota" call

Exhaustive grep across `chunks/*.js` finds **no** `getUsage` / `fetchQuota` / `GET /quota` / `/api/v1/usage`
/ `/entitlement` / `/subscription` endpoint. Qwen Code (like gemini-cli) does **not** poll a remote quota
service. Usage/quota is handled three ways, all measured: **[M]**

1. **Local token accounting** — OpenAI-compatible chat completions return a `usage` block
   (`prompt_tokens`, `completion_tokens`, `total_tokens`). `packages/core/src/services/usageHistoryService.ts`
   accumulates it locally; the `/stats` UI renders it. No network call. **[M]**
2. **Reactive quota-exhaustion detection** — quota state is read off the chat completion's HTTP **error**,
   not a poll (see below). **[M]**
3. **Reset window** — taken from the `Retry-After` / `retry-after-ms` response **header** on 429/503. **[M]**

> Implication for corral: `Usage()` for Qwen must be **local + reactive**, not a remote GET. See plan below.

## The inference endpoint (what auth rides on)

Chat = OpenAI-protocol `POST {baseURL}/chat/completions` via the DashScope-compatible provider. **[M]**
`baseURL` depends on the selected plan/auth (`chunk-AGZF43UV.js`):

| Plan (auth) | baseURL | Cred source |
|---|---|---|
| **qwen-oauth** (free/personal) | derived at runtime from OAuth token field `resource_url` + `/v1` (`getCurrentEndpoint`); default `DEFAULT_DASHSCOPE_BASE_URL` if absent | `~/.qwen/oauth_creds.json` |
| **Coding Plan** (individual · "Weekly quota included") | `https://coding.dashscope.aliyuncs.com/v1` (CN) · `https://coding-intl.dashscope.aliyuncs.com/v1` (Intl) | key `sk-sp-…`, env `BAILIAN_CODING_PLAN_API_KEY` |
| **Token Plan** (teams · usage-based billing) | `https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1` | env `BAILIAN_TOKEN_PLAN_API_KEY` |
| **Standard API key** | `https://dashscope.aliyuncs.com/compatible-mode/v1` (+ `-intl`, `-us`, `cn-hongkong`) | env `DASHSCOPE_API_KEY` |

All are recognized as DashScope providers by `isDashScopeProvider()` (`chunk-BOAWGUY6.js:7100`): host is
`*.dashscope.aliyuncs.com`, `token-plan.*.maas.aliyuncs.com`, `*.alibaba-inc.com`/`*.aliyun-inc.com`, or authType `qwen-oauth`. **[M]**

## Auth source + request headers (measured)

`DashScopeOpenAICompatibleProvider.buildHeaders()` (`chunk-BOAWGUY6.js:7130`): **[M]**

- `Authorization: Bearer <apiKey | OAuth access_token>`  ← set as the OpenAI SDK `apiKey`; for qwen-oauth the
  token is injected per-request via `executeWithCredentialManagement` (auto-refresh + retry on auth error).
- `X-DashScope-AuthType: <authType>`  (e.g. `qwen-oauth`) — **the distinguishing header**.
- `X-DashScope-CacheControl: enable`
- `X-DashScope-UserAgent: QwenCode/<version> (<platform>; <arch>)`
- `User-Agent: QwenCode/<version> (<platform>; <arch>)`

## OAuth device flow (the credential machinery)

`packages/core/src/qwen/qwenOAuth2.ts` (`chunk-TENOUWY2.js:640+`): **[M]**

- Base `https://chat.qwen.ai`
- Device code: `POST https://chat.qwen.ai/api/v1/oauth2/device/code`
- Token / refresh: `POST https://chat.qwen.ai/api/v1/oauth2/token`
- `client_id = f0304373b74a44d2b584a3fb70ca9e56`
- `scope = openid profile email model.completion`
- `grant_type = urn:ietf:params:oauth:grant-type:device_code`, PKCE **S256**
- Token response fields: `access_token`, `refresh_token`, **`resource_url`** (→ becomes the API endpoint),
  `token_type`, expiry.

## Credential / config file corral must read

- **OAuth creds:** `~/.qwen/oauth_creds.json` — `getQwenCachedCredentialPath() = Storage.getGlobalQwenDir()/oauth_creds.json`,
  `QWEN_DIR = ".qwen"` (`chunk-AVQDCKNF.js:264,524`; `chunk-TENOUWY2.js:1317`). Lock: `oauth_creds.lock`.
  JSON carries `access_token`, `refresh_token`, `resource_url`, `token_type`, `expiry_date`. **[M]**
- **API-key providers:** key from env (`DASHSCOPE_API_KEY` / `BAILIAN_CODING_PLAN_API_KEY` /
  `BAILIAN_TOKEN_PLAN_API_KEY`) or `~/.qwen/.env` / `~/.qwen/settings.json`. **[M]**

## Response fields that carry consumption / limit / reset

- **Consumption:** OpenAI `usage.{prompt_tokens, completion_tokens, total_tokens}` on each chat completion. **[M]**
- **Limit / remaining:** **none returned by the API.** Qwen exposes no `remaining`/`limit`/`balance` counter. **[M]**
- **Exhaustion signal (the only "limit" signal):** on the chat call's HTTP error —
  `isQwenQuotaExceededError` (`chunk-AGZF43UV.js:34215`): `status === 429 && code === "insufficient_quota" &&
  message.toLowerCase().includes("free allocated quota exceeded")` (qwen-oauth).
  Gemini lineage: `isProQuotaExceededError` / `isGenericQuotaExceededError` match
  `"Quota exceeded for quota metric 'Gemini … Pro Requests'"`. **[M]**
- **Reset window:** `Retry-After` (seconds or HTTP-date) and `retry-after-ms` header on 429/503; parsed by the
  rate-limit/retry layer (`getRateLimitRetryDelayMs`, retryAfterMode `"minimum"`;
  `RATE_LIMIT_ERROR_CODES = {429, 503, 1302, 1305}`). Error payload also exposes `code`, `message`, `requestId`. **[M]**

### "Approaching the safe threshold"

There is **no** proactive remaining/limit field to watch — Qwen only tells you once you are already at 429.
So "approaching threshold" **cannot be derived from Qwen's API**; corral must track it client-side against a
configured budget (Coding Plan = weekly quota; Token Plan = usage-based, no cap). **[M] that no field exists / [I] that a client-side budget is the only option.**

## ACP support

**Yes.** Flags `--acp` (and deprecated `--experimental-acp`, which warns and forwards) — `chunk-DXXNMPJH.js:4504,4507,4730`.
Entry `runAcpAgent()` in `chunks/acpAgent-PKCTYWJV.js:6730` (`packages/cli/src/acp-integration/acpAgent.ts`), with
full agent-client-protocol session / filesystem / permission integration (Zed-style editors). **[M]**

## corral UsageReporter plan (Qwen provider)

Because Qwen has no queryable usage endpoint, implement `Usage()` as **local + reactive**:

1. **Resolve creds/endpoint.** If `~/.qwen/oauth_creds.json` exists → OAuth: base = `resource_url` + `/v1`,
   `Authorization: Bearer access_token`, `X-DashScope-AuthType: qwen-oauth`; refresh via
   `POST https://chat.qwen.ai/api/v1/oauth2/token` (client_id `f0304373…`, PKCE) when `expiry_date` passed.
   Else read the plan's env key + fixed base URL from the table above.
2. **Track consumption locally.** Sum `usage.total_tokens` from every chat completion response (mirror
   `usageHistoryService`); persist per-session/day. This is corral's "used" number.
3. **Detect exhaustion reactively.** Classify chat errors: qwen-oauth → `429 + code "insufficient_quota" +
   "free allocated quota exceeded"`; Gemini → message contains `"Quota exceeded for quota metric"`.
4. **Reset time.** On that 429, parse `Retry-After` / `retry-after-ms` → reset timestamp.
5. **Threshold alert.** Compute `used / configured_budget`; alert at e.g. 80/90%. Qwen returns no server-side
   remaining, so the budget is a corral config value (weekly for Coding Plan). **[I]**
6. **Optional live probe.** A minimal 1-token `POST {base}/chat/completions` with the DashScope headers reads
   back `usage` and surfaces a 429 early — but there is no cheaper official quota probe. **[I]**

## Confidence

**HIGH** — endpoints, headers, auth types, OAuth flow, credential file, plan base URLs, and ACP are all
directly measured in the v0.19.6 bundle; the "no remote usage/quota GET" conclusion rests on an exhaustive
negative grep. Only the client-side threshold heuristic and the optional probe are inferred.
