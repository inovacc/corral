// Package agents is a provider-agnostic agent runtime: it runs a caller-supplied
// roster of declarative agents through a pluggable Provider backend, keeping
// backends warm via a SessionPool. The host application registers its own agents
// with Register and resolves a backend with ProviderByName.
//
// Backends are subscription coding agents, each in its own sibling package so
// the vendors stay cleanly separated:
//   - github.com/inovacc/agents/agy    — Google Antigravity (ConPTY Driver)
//   - github.com/inovacc/agents/codex  — OpenAI Codex (headless exec) + rate-limit usage tracking
//   - github.com/inovacc/agents/claude — Anthropic Claude Code (headless print)
//
// Providers self-register here via RegisterProvider in their init(); this core
// never imports them, so the graph stays acyclic (the host-registry pattern).
// Import github.com/inovacc/agents/all to populate the provider registry, then
// resolve with ProviderByName. github.com/inovacc/agents/host packages a roster
// + an MCP manifest into an installable plugin tree per host.
//
// Design convention (minimal interface + optional capabilities by type
// assertion, lazy-factory registry) follows inovacc/lensr/pkg/aihost (BSD-3).
package agents
