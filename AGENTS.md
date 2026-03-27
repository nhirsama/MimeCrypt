# Repository Guidelines

## Project Structure & Module Organization
- `cmd/mimecrypt/`: CLI entrypoint (`main.go`), signal-aware process startup.
- `internal/cli/`: Cobra command tree (`login`, `logout`, `download`, `process`, `run`).
- `internal/modules/`: Core pipeline modules (`login`, `discover`, `download`, `encrypt`, `process`, `writeback`, `logout`).
- `internal/provider/` and `internal/providers/`: provider interfaces and implementations (currently `graph`).
- `internal/auth/`, `internal/mail/`, `internal/mimefile/`, `internal/appconfig/`: auth, message fetch, MIME storage, and env config support.
- Keep new code inside `internal/` unless it must be publicly reusable.

## Build, Test, and Development Commands
- `go run ./cmd/mimecrypt --help`: list CLI commands and flags.
- `go build -o mimecrypt ./cmd/mimecrypt`: build local binary (ignored by `.gitignore`).
- `go test ./...`: run all unit tests.
- `go test ./... -cover`: quick coverage check before PR.
- Typical local run:
  - `export MIMECRYPT_CLIENT_ID=<client-id>`
  - `go run ./cmd/mimecrypt login`
  - `go run ./cmd/mimecrypt run --once --output-dir ./output`

## Coding Style & Naming Conventions
- Language: Go (`go 1.26.1`); always format with `gofmt` (or `go fmt ./...`) before committing.
- Follow Go naming rules: exported identifiers in `PascalCase`, internal helpers in `camelCase`.
- Prefer small `Service` structs with explicit dependencies (see `internal/modules/*/service.go`).
- Keep provider-specific logic in `internal/providers/*`; shared contracts belong in `internal/provider/contracts.go`.

## Testing Guidelines
- Use Go’s `testing` package with file pattern `*_test.go` next to tested packages.
- Name tests as `Test<Behavior>` (examples: `TestRunDetectsPGPMIME`, `TestSessionLoginAndRefresh`).
- Use `t.Parallel()` for independent tests where possible.
- Add tests for new module behavior and provider edge cases before wiring CLI flags.

## Commit & Pull Request Guidelines
- Existing history follows Conventional Commit prefixes: `feat:` and `docs:`. Keep this style (`feat:`, `fix:`, `refactor:`, `test:`, `docs:`).
- Keep commits focused by module (for example, `feat: add writeback verification in process module`).
- PRs should include:
  - purpose and scope,
  - key commands run (for example `go test ./...`),
  - config/env changes (`MIMECRYPT_*`),
  - CLI output snippets when command behavior changes.

## Security & Configuration Tips
- Never commit secrets, tokens, or local state (`graph-token.json`, `sync-*.json`, `output/*.eml`).
- Treat `output/` artifacts as potentially sensitive plaintext during development.
