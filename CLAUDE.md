# CLAUDE.md

<!-- rev:001 -->

Claude Code entry point for the **corral** module (`github.com/inovacc/corral`). The canonical, cross-tool agent instructions live in **AGENTS.md** (imported below).

@AGENTS.md

## Claude-Code-only

- **This is a library, not an app.** Consumers import `github.com/inovacc/corral` and drive coding-agent CLIs behind the `Provider` interface; the thin CLI lives at `cmd/corral`.
- **The agent roster is caller-supplied.** corral ships the machinery (`Register`/`All`/`ByName`, provider registry, `SessionPool`, `Agency`, `Monitor`); the caller decides which agents to register.
- Keep shared build/test/style/security rules in AGENTS.md — do not duplicate them here.
