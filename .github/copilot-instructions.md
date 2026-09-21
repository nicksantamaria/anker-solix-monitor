# Copilot instructions for anker-solix-monitor

Follow these repository-specific rules for all changes.

## Repository patterns

- Keep the current package boundaries:
  - `cmd/` for binaries (`solix-cli`, `solix-monitor`)
  - `internal/` for monitor service internals (API, config, database, monitor loop)
  - `pkg/solix` for reusable SDK code
- Keep binaries thin; put reusable logic in `internal/` or `pkg/solix`.
- Use dependency injection and small interfaces for testability, matching existing monitor/API patterns.
- Preserve the project’s package-level doc comments and clear exported type/function docs.

## Go best practices (required)

- Prefer the standard library first; only add dependencies when necessary.
- Pass `context.Context` explicitly and honor cancellation/timeouts.
- Return wrapped errors with `%w` and useful operation context.
- Use structured logging with `log/slog` key/value fields (no unstructured printf-style logging in service code).
- Keep functions focused and side effects explicit.
- Add or update tests for behavioral changes using the existing `go test ./...` workflow.
- Ensure formatting/lint expectations remain valid (`gofmt`, `go vet`).

## Pull request title format (required for release-please)

All pull request titles **must** use release-please compatible Conventional Commit syntax:

- `<type>: <description>`
- `<type>(<scope>): <description>`
- `<type>!: <description>` (breaking change)
- `<type>(<scope>)!: <description>` (breaking change with scope)

Allowed `<type>` values: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `build`, `ci`, `perf`, `revert`.

Examples:
- `feat(api): add bucketed history response caching`
- `fix(monitor): handle disconnect without stale connected state`
- `chore: update sqlite dependency`

If a change is breaking, include `!` in the title and describe the breaking change in the PR body.
