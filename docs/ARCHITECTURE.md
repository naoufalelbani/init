# Architecture

The executable entrypoint is `cmd/ai-init`. Command behavior currently lives in `internal/cli`; repository fact detection and safe generation are deterministic and use the Go standard library. Process execution is argument-based. Future extraction should place analyzer, filesystem, adapter, and worktree domain logic in separate internal packages as their behavior grows.

Generated files are editable output. Existing custom `AGENTS.md` is not modified. Managed output is marked and regenerated as a whole, with a backup on update.
