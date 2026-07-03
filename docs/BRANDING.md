# Agent-Runtime Branding Names

Branding reference for the module currently at the provisional
`github.com/inovacc/agents`. **Subject:** a provider-abstracted Go runtime that
drives subscription coding-agent CLIs (Anthropic Claude Code, OpenAI Codex,
Google Antigravity) behind one interface — warm session pool, rate-limit/quota
awareness, pluggable provider registry, per-host plugin installer. The roster of
agents is caller-supplied; the library ships the machinery.

> **Recommendation: `Gantry`** — a gantry is a fixed frame that positions and
> drives multiple tools over one shared work area; this module frames and drives
> multiple coding-agent CLIs behind one interface. Distinctive, technical, short
> (`go get github.com/inovacc/gantry`), and sidesteps the over-used
> agent/agency/orchestrator cluster. Strong alternates: **Manifold**, **Rig**.

## Project Name Candidates

| Name | Rationale |
|------|-----------|
| **agents** (current) | Accurate but generic — collides with the entire "agent/agents/agency/orchestrator" cluster; poor for `go get` discoverability. Baseline for comparison. |
| **Gantry** ⭐ | A gantry frames and drives multiple tools over one work area → multiple agent CLIs behind one interface. Distinctive, technical, uncrowded. |
| **Manifold** | An intake manifold merges many pipes into one → many CLI backends, one API. Precise metaphor for "many→one." (Minor ML/math baggage — check `go get` collision.) |
| **Rig** | You "rig up" and drive heavy equipment; short, punchy, uncommon in this space. `github.com/inovacc/rig`. |
| **Harness** | Holds and drives multiple engines — "harness a coding CLI." Clean and evocative; slightly common as a word. |
| **Provender** | Portmanteau of **provider** + the archaic "provisions"; brandable, memorable, and literally names the provider abstraction. |
| **Muster** | To muster is to assemble *and* dispatch a roster — exactly the register→run flow. Uncommon, apt, one syllable of energy. |
| **Relay** | Relays prompts to CLI backends and keeps the channel warm. Simple, technical. |
| **Conduit** | A single channel to many backends. Neutral, professional. |
| **Tandem** | Drives multiple providers "in tandem." Friendly, memorable. |
| **Loom** | Weaves many provider threads into one fabric; warm sessions held on the loom. Evocative — but **flag:** Java's "Project Loom" and other `loom` projects exist. |
| **Corral** | Corral a herd of agent CLIs behind one gate. Playful; may read too casual for the technical tone. |

## Feature / Component Names

| Component | Current | Branded options |
|-----------|---------|-----------------|
| Provider backend | `Provider` | Rig · Backend · Driver |
| Warm session cache | `SessionPool` | Hearth · WarmPool · Kiln |
| Orchestrator | `Agency` | Deck · Console · Bridge |
| Rate-limit gate | `Monitor` / `checkLimit` | Governor · Throttle · Warden |
| Health checks | `Doctor` | Preflight · Checkup · Doctor |
| Plugin installer | `host/` | Berth · Dock · Mount |

## Taglines

- **One harness, every coding agent.**
- **Claude, Codex, Antigravity — one interface, kept warm.**
- **Bring your own roster; we ship the machinery.**
- **Drive any coding CLI. Keep it warm.**
- **The runtime under your agents.**
- **Subscription agents, unified.**
- **Warm sessions. Rate-limit aware. Provider-agnostic.**
- **One API for every AI CLI.**

## CLI Branding Themes (`cmd/<name>`)

**Infrastructure** (recommended — matches Gantry/Rig):
```
<name> mount     # install the plugin tree into a host (was: host install)
<name> spin      # run an agent (warm session)
<name> preflight # health checks (was: doctor)
<name> gauge     # show provider rate-limit / quota status
```

**Minimal** (verbs only):
```
<name> run       # run an agent
<name> check     # doctor
<name> install   # host installer
<name> usage     # limit status
```

**Nautical** (if Deck/Bridge components are adopted):
```
<name> board     # register + run an agent
<name> berth     # install into a host
<name> soundings # health + quota checks
```

## Color Palette

Structural-steel blue (the frame) with a warm amber (the warm session) — cool
machinery holding something warm.

| Role | Name | Hex |
|------|------|-----|
| Primary | Gantry Steel | `#3B4A6B` |
| Secondary | Girder Blue | `#5B7DB1` |
| Accent | Warm Session Amber | `#E8A13A` |
| Warning | Rate-Limit Rust | `#D0533B` |
| Muted | Slate Rail | `#8A93A6` |

## Logo Concepts

1. **Gantry over three heads** — a minimal gantry-crane frame spanning three tool-heads (Claude / Codex / Antigravity), one hoist descending to a single glowing node. Frames "many tools, one driver."
2. **Convergence** — three thin lines entering from the left, merging into one bold node on the right (many CLIs → one interface); the merge point glows amber (warm session).
3. **Warm node in a bracket** — a bold `[ ]` bracket cradling a filled amber circle: the runtime holding a session warm.
4. **Monogram** — a `G` whose crossbar extends into a gantry rail, or an `M`/`R` for the chosen name, in Gantry Steel with an amber joint.

## Brand icon (iconforge)

Generate once the name is chosen:
```bash
iconforge forge --generate \
  --name gantry \
  --primary "#3B4A6B" --secondary "#5B7DB1" --accent "#E8A13A" \
  --output build/icons
```
(Not run here — pending the final name pick.)

---

## Rename procedure (once a name is chosen)

The provisional `agents` name is a cheap find/replace since it was scaffolded
provisionally:
1. `go mod edit -module github.com/inovacc/<name>`
2. Rewrite import paths `github.com/inovacc/agents` → `github.com/inovacc/<name>`
   across `*.go` (root + `agy/ codex/ claude/ all/ host/ cmd/agents/`).
3. Rename `cmd/agents/` → `cmd/<name>/` and update `.goreleaser.yaml` `main:` +
   `binary:`.
4. Rename the directory `…/projects/agents` → `…/projects/<name>` (optional).
5. `go build ./... && go test ./...` to confirm.
