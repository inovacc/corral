# Antigravity (`agy`) — quota + auth mechanism (dissect-confirmed)

Recovered via `unravel app dissect` + `unravel garble strings` of
`%LOCALAPPDATA%\agy\bin\agy.exe` — **Antigravity CLI 1.0.15**, Go
`go1.27-20260615-RC00` PE, Google-signed (DigiCert Trusted Root G4), not garbled,
symbol-stripped. Supersedes the agy section of `claude-codex-agy.md` for the
usage/auth surface. Dated RE record.

## Quota / "Models & Quota" call

- **Endpoint:** `POST https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary`
  (canary host `daily-cloudcode-pa.googleapis.com`). Symbols:
  `RetrieveUserQuotaSummaryRequest` / `…Response`.
- **Response shape:** `RetrieveUserQuotaSummaryResponse` → `QuotaSummaryGroup[]`
  (one group per **model family** — "GEMINI MODELS": Gemini Flash/Pro; "CLAUDE AND
  GPT MODELS": Claude Opus/Sonnet, GPT-OSS) → `QuotaSummaryBucket[]` (one per
  window: **Weekly Limit**, **Five Hour Limit**) → **`RemainingFraction`** (0–1).
- **Semantics:** the CLI's "Models & Quota" view renders `RemainingFraction` as
  **"% available"** — 100.00% = full quota, nothing used. So corral's mapping
  `UsedPercent = (1 − RemainingFraction) × 100` (clamped 0–100) is **correct**; a
  fresh account reads ~0% used across all four windows.
- Related model: `Subscription` / `SubscriptionActive` / `SubscriptionType` /
  `Entitlement` / `PlanId` / `UserTier` (e.g. "Google AI Pro").

## Auth (Google OAuth2)

- **Library:** Go `golang.org/x/oauth2` — `oauth2.Config` + `TokenSource` (symbols
  `oauth2.Config`, `TokenSource`, `grant_type`, `refresh_token`).
- **Credential file:** `~/.gemini/oauth_creds.json` (`access_token`,
  `refresh_token`, `expiry_date` ms; `~/.gemini` config dir, `.GeminiDir`).
- **OAuth client:** client_id `1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com`
  (Antigravity) and `884354919052-…apps.googleusercontent.com` (gemini-cli lineage);
  embedded **client_secret `GOCSPX-…`** (present in-binary; redacted here).
- **Endpoints:** auth `https://accounts.google.com/o/oauth2/auth`, token
  `https://oauth2.googleapis.com/token`.
- **Refresh behaviour (the key finding):** agy refreshes the access token
  **in-memory on every run** via the `oauth2.Config` `TokenSource`
  (`grant_type=refresh_token` → `oauth2.googleapis.com/token`). It does **not**
  depend on `oauth_creds.json` being fresh — which is why the CLI shows live quota
  even when that file's `access_token` is days-expired.

## corral fix (agy `UsageReporter`) — attempted, blocked by entitlement

The endpoint + `RemainingFraction` mapping already match; the gap is the expired
`~/.gemini` token. A token refresh was **implemented and live-tested** (2026-07-04)
and the finding is that agy's quota is **NOT standalone-reproducible**:

- **The `~/.gemini/oauth_creds.json` token is minted by gemini-cli's PUBLIC
  installed-app OAuth client** — `681255809395-…apps.googleusercontent.com` (its
  `GOCSPX-…` "secret" is published in the open-source gemini-cli repo, not
  confidential). Live-verified: a `grant_type=refresh_token` against
  `oauth2.googleapis.com/token` with that client → **200** (fresh access token).
- agy's OWN embedded client (`1071006060591-…` / `884354919052-…` +
  in-binary `GOCSPX-…`) returns **401 `invalid_client`** — its secret is not the
  one bound to the on-disk refresh token.
- BUT the refreshed gemini-cli token → `retrieveUserQuotaSummary` returns
  **403** — it lacks the **Antigravity-Pro entitlement**. agy mints a *separate*,
  entitled token in-memory (via its own OAuth flow) that is **never persisted**
  (confirmed: `~/.gemini/antigravity-cli/` holds logs/conversations/skills but
  **no token file**; the only on-disk token is the expired gemini-cli one).

**Conclusion:** corral cannot reproduce agy's "Models & Quota" from files alone —
the entitled token lives only in agy's process memory. So `agy/usage.go` keeps the
read-only + honest-error behaviour (expired → "run `agy`"; 403 → entitlement note)
rather than shipping a refresh that always 403s. Revisit only if agy starts
persisting its entitled token, or exposes a local endpoint.
