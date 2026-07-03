# Known Issues

Known limitations and rough edges. These are documented constraints, not bugs to
be surprised by.

## Providers need auth before a live turn

The `grok`, `kimi`, and `qwen` presets compile and self-register (via each
package's `init()` calling `corral.RegisterProvider`), so `ProviderByName`
resolves them and a turn will spawn the CLI — but the underlying binary needs an
authenticated session before a live turn returns anything useful:

- **grok** — run `grok login` first (registered under `grok`, alias `xai`).
- **kimi** — needs an active Moonshot subscription (aliases `kimi-code`,
  `moonshot`).
- **qwen** — needs an auth type configured (`--auth-type` flag or `settings.json`);
  without it the gemini-cli-fork's non-interactive mode fails (alias `qwen-code`).

## Oversized prompts route to stdin (grok/kimi stdin unverified)

`PromptFlag` providers (`grok --single`, `kimi --prompt`, `qwen --prompt`) pass
the prompt as the flag's value. When a composed prompt exceeds ~8 KB
(`maxArgPrompt = 8000` in `cli.go`) it is fed on **stdin** instead, to stay under
the OS command-line length limit (Windows ~32 KB via `CreateProcess`). Qwen
documents reading the prompt from stdin, but grok's and kimi's stdin handling is
**unverified** — an oversized prompt to those two may not be consumed as expected.

## qwen standalone installer checksum failure on Windows

The standalone `qwen` installer failed a SHA-256 checksum verification on Windows.
Install via npm instead:

```bash
npm i -g @qwen-code/qwen-code
```

## agy is Windows-only

The Antigravity (`agy`) provider drives a ConPTY pseudo-console (Antigravity has
no headless mode). ConPTY is Windows-only: `run_windows.go` carries the real
implementation, while `run_other.go` is a non-Windows build-tagged stub whose
`run` returns `agy: ConPTY driver is Windows-only` and whose session falls back to
an effectively unusable one-shot. `agy` is usable only on Windows.

## No usage/quota tracking for grok/kimi/qwen

`claude`, `codex`, and `agy` implement `UsageReporter` (usage/limit polling). The
`grok`, `kimi`, and `qwen` presets do **not** yet — `Monitor`/`checkLimit` returns
no `LimitStatus` for them, so rate-limit awareness is unavailable on those three.
