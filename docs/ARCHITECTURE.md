# Architecture

The executable entrypoint is `cmd/ai-init`. Command behavior currently lives in `internal/cli`; repository fact detection and safe generation are deterministic and use the Go standard library. Process execution is argument-based. Future extraction should place analyzer, filesystem, adapter, and worktree domain logic in separate internal packages as their behavior grows.

Generated files are editable output. Existing custom `AGENTS.md` is not modified. Managed instruction and context blocks have visible markers; updates replace only those blocks and retain surrounding manual notes. Backups are kept on updates.

Persistent context is generated under `.ai/` with `PROJECT_MAP.md`, `ARCHITECTURE.md`, `CONVENTIONS.md`, `CURRENT_STATE.md`, and `DECISIONS.md`. It records manifest, directory, configuration, Git, and recognized indexer evidence. Architecture boundaries and design decisions are explicitly left unknown unless source evidence supports them.
