# AGENTS.md
<!-- rev:003 -->

Canonical cross-tool contributor and agent instructions for **corral**
(`github.com/inovacc/corral`) — a provider-abstracted Go runtime that drives
subscription coding-agent CLIs (Claude Code, Codex, Antigravity, Grok, Kimi)
behind one `Provider` interface, with a warm `SessionPool`, rate-limit
awareness, a pluggable provider registry, and a per-host plugin installer. The
agent roster is caller-supplied; the library ships the machinery. Go 1.26.3,
BSD-3-Clause. Two external runtime deps (amended 2026-07-05, was one): `github.com/UserExistsError/conpty` (subscription CLI ConPTY driver) and `github.com/OpenRouterTeam/go-sdk` v0.5.9 (the `api` execution mode's OpenRouter backend — pulled in only when the `openrouter` subpackage is imported; Apache-2.0, beta, Go 1.25+). The hand-rolled `apiprovider` (OpenAI/Anthropic) backends add no further deps; a backend's SDK is imported only by its own provider package, so the core stays SDK-free.

## Build

```bash
go build ./...   # or: task build  (injects version via -ldflags)
```

The root package `corral` holds the core types; each `<vendor>/` subpackage is a
provider; `cmd/corral` is a thin CLI on `github.com/inovacc/mantle`.

## Test

```bash
go test ./...        # or: task test        (fast, -short)
task test:full       # -race + coverage.out
task test:cover      # print total coverage %
```

Current coverage: **61.2%** (target 80%). Add tests with any behavior change; a
provider's live turn needs real auth, so gate network paths behind `-short`.

## Lint

```bash
golangci-lint run    # or: task lint  (task lint:fix to autofix)
```

Enabled linters: `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`,
`misspell`. `task check` runs fix + fmt + vet + lint + test.

## Code style

- Idiomatic Go; `gofmt` clean (`task fmt`). Keep functions small and cohesive.
- Wrap errors with `%w` (`fmt.Errorf("open session: %w", err)`); compare with
  `errors.Is` / `errors.As`, never `==`. Messages lowercase, no trailing period.
- **Providers self-register** via `init()` calling `corral.RegisterProvider` and
  are blank-imported by `all/all.go`. The core package **never imports** a
  vendor package — the dependency graph stays acyclic and populated only when a
  consumer imports `corral/all` (or an individual provider).
- Prefer returning a `*corral.CLIProvider` preset; only write a custom
  `Provider` when you need extra capability (e.g. embedding the preset to add a
  `UsageReporter`, as `claude/` does).
- Exported types/functions carry doc comments; `log/slog` for logs; stdout is
  reserved for agent output data.

## Adding a provider

1. **Create `<vendor>/<vendor>.go`** in a new package. Return a
   `*corral.CLIProvider` preset (set `ProviderName`, `Bin`, and the relevant
   `BaseArgs` / `ModelFlag` / `SchemaFlag` / `OutputFlag` / `PromptFlag`), or a
   custom type embedding the preset for extra capabilities.
2. **Register in `init()`:** `corral.RegisterProvider(func() corral.Provider {
   return New() }, "<vendor>", "<alias>", ...)` — first name is canonical, the
   rest are aliases.
3. **Blank-import in `all/all.go`:** add
   `_ "github.com/inovacc/corral/<vendor>"` so the barrel populates the registry.
4. **Wire the prompt:** headless CLIs that take the prompt as a flag set
   `PromptFlag` (e.g. grok `--single`, kimi `--prompt`); otherwise the
   prompt is passed positionally. Oversized prompts fall back to stdin
   automatically (`cli.go`). `SchemaFlag` empty means embed the schema in the
   prompt and parse JSON from stdout.

## Security

- **No secrets in-repo.** Never commit tokens, cookies, or `.env` files.
- Each backend CLI authenticates via its **own config/login** (e.g.
  `~/.claude/.credentials.json`, `codex` login, `grok login`, Moonshot
  subscription). corral reads those credentials read-only;
  it never stores or transmits them elsewhere.
- `UsageReporter` calls hit each vendor's own usage endpoint with the CLI's
  existing bearer; absence of login is reported as `(nil, nil)`, never fatal.

## Commit / PR conventions

- **Conventional Commits** (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`,
  `chore:`), imperative subject, no AI attribution.
- Keep PRs focused; `task check` must pass (fmt, vet, lint, tests green).
- Update `README.md` / `docs/` when adding a provider or changing public API.
- License is **BSD-3-Clause** (© inovacc); new files inherit it.
