# AGENTS.md

## Repository purpose
`ai-init` is a cross-platform Go CLI that initializes AI coding environments in existing repositories while preserving their architecture and user changes.

## Development
- Keep command parsing in `internal/cli` and invoke processes with `exec.Command` argument lists.
- Preserve idempotency and safe file handling. Never silently replace custom instructions.
- Keep support claims in README aligned with implemented behavior.
- Use the Go standard library unless a clear need justifies a dependency.
- Run `gofmt` and `go test ./...` for changes.
