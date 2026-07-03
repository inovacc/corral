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
// Usage/quota monitoring (corral.UsageReporter) is not yet wired — Grok's quota
// lives behind the grok.com gateway; add a usage.go once the endpoint is
// confirmed.
package grok
