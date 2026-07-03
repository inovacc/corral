# Contributors
<!-- rev:001 -->

Thanks for your interest in **corral** (`github.com/inovacc/corral`) — a
provider-abstracted runtime that drives subscription coding-agent CLIs behind
one `Provider` interface. This guide covers how to build, test, and submit
changes.

## Maintainer

| Name | GitHub | Role |
|------|--------|------|
| inovacc | [@inovacc](https://github.com/inovacc) | Owner / Maintainer |

## Prerequisites

- **Go 1.26+** (toolchain pinned in `go.mod`).
- [`task`](https://taskfile.dev) (optional but preferred — targets live in `Taskfile.yml`).
- [`golangci-lint`](https://golangci-lint.run) for linting.

## Development workflow

Clone the repo, then use the Taskfile targets (or the raw Go commands):

```bash
task build   # go build -ldflags "-X main.version=..." ./...
task test    # go test -short ./...
task lint    # golangci-lint run ./...
task check   # fix + fmt + vet + lint + test
```

Equivalent without `task`:

```bash
go build ./...
go test ./...
golangci-lint run
```

Run the full suite with the race detector and coverage before opening a PR:

```bash
task test:full   # go test -race -coverprofile=coverage.out ./...
```

Keep modules tidy after touching dependencies:

```bash
task deps        # go mod download && go mod tidy && go mod verify
```

## Coding standards

- Format with `gofmt` (`task fmt`) and keep `go vet` (`task vet`) clean.
- Fix lint findings before submitting; `task lint:fix` applies autofixes.
- New providers self-register via `init()` and are blank-imported by `all/`;
  follow the existing packages (`claude/`, `codex/`, `grok/`, …) as templates.
- Add tests alongside changes; coverage is currently 61.2% and should not regress.

## Commit messages

This project uses [Conventional Commits](https://www.conventionalcommits.org):

```text
feat(grok): add UsageReporter
fix(session): cap retry attempts
docs(contributors): expand contributing guide
```

Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `ci`.

## Pull requests

1. Fork and branch from `main`.
2. Make focused, targeted changes with tests.
3. Ensure `task check` passes locally.
4. Open a PR with a clear description and a Conventional-Commit-style title.

## License

By contributing, you agree that your contributions are licensed under the
project's **BSD 3-Clause License** (see [`LICENSE`](../LICENSE)),
Copyright (c) 2026 inovacc.
