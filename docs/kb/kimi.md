# KB: Moonshot Kimi Code CLI — usage/quota reverse-engineering

<!-- rev:001 -->

> Goal: replicate Kimi Code's own usage/quota reporting as a corral `UsageReporter`.
> Legend: **[M]** = measured (found in the shipped binary / bundle) · **[I]** = inferred.

## Product / binary

- **Product:** Moonshot **Kimi Code** CLI (`kimi`). **[M]**
- **Version:** **0.22.2** (installed); update channel reports latest `0.22.2`
  (`~/.kimi-code/updates/latest.json`, source `cdn`). **[M]**
- **Binary type:** **Bun-compiled single-executable** PE — `MZ` header, embedded `Bun` runtime markers,
  bundles **Node v24.15.0**. The whole app is a **plaintext (un-minified-ish, pretty-tabbed) JS bundle**
  embedded in the executable, so it greps directly — no unpacking needed. **[M]**
- **Binary path / size:** `C:/Users/dyamm/.kimi-code/bin/kimi.exe`, **126,048,256 bytes (~120 MiB)**. **[M]**
- **Config home:** `~/.kimi-code/` — `config.toml` (agent/runtime; empty until login), `tui.toml` (client prefs),
  `device_id`, `credentials/` (+ `credentials/mcp/`), `sessions/`, `cache/`, `logs/`, `telemetry/`, `updates/`. **[M]**
- **Two API surfaces:** the **managed** Kimi-for-Coding plan (`api.kimi.com/coding/v1`, OAuth) and **BYO Moonshot
  API key** (`api.moonshot.ai/v1` / `api.moonshot.cn/v1`, `KIMI_API_KEY`). The `/usages` quota call is a
  **managed-plan feature**; a raw Moonshot key hits it and 404s. **[M]**

## HEADLINE — Kimi HAS a dedicated remote usage/quota endpoint

Unlike Qwen/gemini-cli, Kimi Code polls a real quota service. The whole path is captured below.

## The usage/quota call (measured)

Source functions in the bundle: `managedUsageUrl()`, `fetchManagedUsage()`, `parseManagedUsagePayload()`,
`buildManagedUsageSection()` (module `/packages/oauth/src/managed-usage`).

- **Endpoint:** `GET {baseUrl}/usages`
  - `managedUsageUrl(baseUrl) → \`${baseUrl.replace(/\/+$/,"")}/usages\``; when `baseUrl` is undefined it falls back to
    `kimiCodeUsageUrl()` = `${kimiCodeBaseUrl()}/usages`.
  - `kimiCodeBaseUrl()` default = **`https://api.kimi.com/coding/v1`** (`DEFAULT_KIMI_CODE_BASE_URL`), overridable via
    env `KIMI_CODE_BASE_URL` / `KIMI_BASE_URL`.
  - ⇒ **Default full URL: `https://api.kimi.com/coding/v1/usages`**. **[M]**
- **Method:** **GET** — `fetch(url, { headers, signal })` with **no `method`/`body`** ⇒ HTTP GET. **[M] (by fetch default)**
- **Auth header:** `Authorization: Bearer ${accessToken}` + `Accept: application/json`. **[M]**
- **Timeout:** 8000 ms via `AbortController` (`opts.timeoutMs ?? 8e3`). **[M]**
- **Error mapping** (`fetchManagedUsage`): non-2xx → `{kind:"error", status, message}` from `readApiErrorMessage`;
  **401** → "Authorization failed. Please check your API key (try /login)."; **404** → "Usage endpoint not available.
  Try Kimi For Coding." (i.e. a non-managed key/base). On 2xx → `parseManagedUsagePayload(await res.json())`. **[M]**

## Auth source — where `accessToken` comes from (measured)

Managed provider name constant: **`KIMI_CODE_PROVIDER_NAME = "managed:kimi-code"`**. **[M]**

- The usage call's token is the **managed OAuth access token**, resolved via
  `accessToken = await host.resolveOAuthToken(KIMI_CODE_PROVIDER_NAME)` → `manager.ensureFresh()`
  (auto-refresh) → `creds.access_token`. **[M]**
- Login is an **OAuth device flow**: `accessToken = await loginWithDevice()` against the OAuth host
  **`https://auth.kimi.com`** (`KIMI_CODE_OAUTH_HOST` default; env `KIMI_CODE_OAUTH_HOST` / `KIMI_OAUTH_HOST`). **[M]**
- Alternative (BYO key) path exists: `accessToken = await this.resolveApiKey()` reading env **`KIMI_API_KEY`** — used
  for direct Moonshot base URLs; that path 404s on `/usages`. **[M]**

## Credential / config file corral must read

- **OAuth token store dir:** **`~/.kimi-code/credentials/`** — `credentialsDir = join(homeDir, "credentials")`. **[M]**
- **Store key:** the managed OAuth entry is keyed
  **`oauth/kimi-code-env-<sha256>`** — `KIMI_CODE_SCOPED_OAUTH_KEY_PREFIX = "oauth/kimi-code-env-"` concatenated with
  `createHash("sha256").update(JSON.stringify({...env-scoping object...}))`. The hash scopes the token to the
  active base-URL/env so different endpoints keep separate tokens. **[M]**
- **Stored value fields:** `access_token`, `refresh_token`, `expires_at`, `token_type`
  (read as `creds.access_token`; refreshed by `ensureFresh()`). **[M]**
- **On-disk file:** the store persists each key as a JSON file under `~/.kimi-code/credentials/`; exact filename is the
  sanitized key (slash-scoped `oauth/kimi-code-env-<hash>`). Corral should resolve the token from this store (or via
  the CLI's own resolver) rather than hard-coding a filename. **[I] (path/format inferred; the `credentials/*.mjs`
  entries in the binary are a bundled google-auth-library, NOT Kimi's store — ignore those.)**
- **API-key mode:** env `KIMI_API_KEY` (+ `KIMI_BASE_URL` / `KIMI_CODE_BASE_URL`). Config `~/.kimi-code/config.toml`
  is populated by login with the managed provider/model entries. **[M]**

## Response fields that carry consumption / limit / reset (measured)

`parseManagedUsagePayload(payload)` shape:

```jsonc
{
  "usage":  { … },        // summary row, labeled "Weekly limit"
  "limits": [ { … }, … ]  // additional limit rows (per window)
}
```

Each row is normalized by `toUsageRow(raw)`:

- **`limit`** — `toInt(raw.limit)`. **[M]**
- **`used`** — `toInt(raw.used)`; **if `used` absent**, computed as **`limit - remaining`** from `toInt(raw.remaining)`.
  So the API may return **either `used` or `remaining`** (+ `limit`). **[M]**
- **`name` / `title`** — row label (else default `"Weekly limit"` for summary, or a computed window label). **[M]**
- **Reset window** (`resetHintFrom(raw)`), checked in order: **[M]**
  - Absolute ISO string: **`reset_at`, `resetAt`, `reset_time`, `resetTime`** → `formatResetTime`.
  - Relative seconds: **`reset_in`, `resetIn`, `ttl`, `window`** → "resets in <duration>".
- **Per-limit window** (`limits[].window`): **`window.duration`** (int) + **`window.timeUnit`** (`"MINUTE"|"HOUR"|"DAY"`),
  plus optional `name`/`title`/`scope`; label rendered as e.g. `"5h limit"`, `"7d limit"`. A `limits[]` item may nest
  its numbers under **`detail`** (`item.detail.{limit,used,remaining,…}`) or carry them flat. **[M]**

### "Approaching the safe threshold" — the exact signal

`buildManagedUsageSection` computes **`usedRatio = used / limit`** per row and colors it via
**`ratioSeverity(ratio)`**: **[M]**

| ratio = used/limit | severity | theme color |
|---|---|---|
| **≥ 0.85** | `danger` | error (red) |
| **≥ 0.50** | `warn`   | warning (yellow) |
| `< 0.50` | `ok` | success (green) |

⇒ **The "approaching limit" trigger corral should mirror is `used/limit ≥ 0.85` (hard warn) with a soft warn at
`≥ 0.50`.** There is a live server-side `limit`+`used`/`remaining`, so this is a real proactive threshold (no need for
a client-side budget guess, unlike Qwen). Context-window usage is a separate local meter (`contextTokens/maxContextTokens`),
not part of `/usages`. **[M]**

## ACP support

**Yes.** `kimi acp` subcommand present — `.command("acp")` runs an **ACP (agent-client-protocol) server over stdio**
for editor integration. **[M]**

## `provider` subcommand

**Yes.** `kimi provider` with **`add <provider>` / `list [provider]` / `remove <provider>`** manages provider configs
(managed Kimi vs BYO Moonshot / custom base URLs). Related auth verbs: **`kimi login`**, **`kimi logout`**,
**`kimi usage`** (the CLI surface for the `/usages` call above). **[M]**

## Relevant hosts / env knobs (measured)

- Managed API base: `https://api.kimi.com/coding/v1` (env `KIMI_CODE_BASE_URL` / `KIMI_BASE_URL`).
- OAuth host: `https://auth.kimi.com` (env `KIMI_CODE_OAUTH_HOST` / `KIMI_OAUTH_HOST`).
- BYO Moonshot: `https://api.moonshot.ai/v1`, `https://api.moonshot.cn/v1` (env `KIMI_API_KEY`).
- Telemetry: `https://telemetry-logs.kimi.com/v1/event`. Docs: `moonshotai.github.io/kimi-code/`.

## corral UsageReporter plan (Kimi Code provider)

`Usage()` is a **single authenticated GET** — the clean case:

1. **Resolve creds.** Read the managed OAuth token from `~/.kimi-code/credentials/` (key
   `oauth/kimi-code-env-<sha256>`, field `access_token`); if `expires_at` is past, refresh against
   `https://auth.kimi.com` (device-flow refresh token). If only `KIMI_API_KEY` is set, note that `/usages` is
   managed-only and will 404.
2. **Resolve base URL.** `KIMI_CODE_BASE_URL` / `KIMI_BASE_URL` env, else default `https://api.kimi.com/coding/v1`.
3. **Call** `GET {base}/usages` with `Authorization: Bearer <access_token>`, `Accept: application/json`, 8 s timeout.
4. **Parse.** `summary = usage{limit, used|remaining}`; `limits[] = {detail{limit,used|remaining}, window{duration,timeUnit}, name}`.
   Normalize each: `used = used ?? (limit - remaining)`; `reset` from `reset_at|resetAt|reset_time|resetTime`
   (absolute) or `reset_in|resetIn|ttl|window` (seconds).
5. **Map to corral's Usage struct:** `Used`, `Limit`, `Remaining = limit-used`, `ResetAt`, plus a `Label`/`Window`
   per row (weekly summary + sub-window limits).
6. **Threshold:** flag `warn` at `used/limit ≥ 0.50`, `danger` at `≥ 0.85` (mirrors `ratioSeverity`).
7. **Errors:** 401 ⇒ needs `/login` (refresh/re-auth); 404 ⇒ not a managed plan (BYO key) — report "no managed quota".

## Confidence

**HIGH** — endpoint (`GET /usages`), base-URL default, bearer-OAuth auth, credential store dir + key prefix + token
fields, the full response schema (`usage`/`limits`/`detail`/`window` + used/remaining/limit + all reset field names),
the 0.85/0.50 severity thresholds, and the `acp`/`provider`/`login`/`logout`/`usage` subcommands are all **directly
measured** in the v0.22.2 Bun binary's JS bundle. Only the exact on-disk credential **filename** and the GET method
(inferred from `fetch` defaults) are not literal string matches.
