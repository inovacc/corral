// Package grok is the x.ai Grok provider for the corral agent runtime: a
// headless `grok --single <prompt>` preset (Grok mirrors Claude Code's CLI
// conventions). Grok exposes no output-schema *file* flag usable here — its
// `--json-schema` takes inline JSON — so schemas are embedded in the prompt and
// the JSON is parsed from stdout.
//
// Install the backend CLI (Windows):
//
//	irm https://x.ai/cli/install.ps1 | iex
//
// Auth: `grok login` (subscription); config under ~/.grok (config.toml, OAuth).
// Verified against grok 0.2.82.
//
// Usage/quota monitoring (corral.UsageReporter) is wired via usage.go: the real
// GET /billing?format=credits the CLI's `/usage` command makes, authenticated
// with the session token in ~/.grok/auth.json (or the XAI_API_KEY BYOK env).
// Grok returns raw credit counters with no server-side percent, so a percent is
// derived only when the response carries an included-credit allowance; otherwise
// usage is surfaced with the percent unknown. See docs/kb/grok.md.
package grok
