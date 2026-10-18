# Architecture

The executable entrypoint is `cmd/ai-init`. Command orchestration lives in `internal/cli`; repository fact detection is isolated in `internal/analyzer`, while adapters, hooks, environment snapshots, and port allocation have separate internal packages. Analysis and safe generation use the Go standard library. Process execution is argument-based. More of the CLI's remaining worktree, integration, and filesystem coordination can be extracted as those domains grow.

Generated files are editable output. Existing custom `AGENTS.md` is not modified. Managed instruction and context blocks have visible markers; updates replace only those blocks and retain surrounding manual notes. Backups are kept on updates.

Persistent context is generated under `.ai/` with `PROJECT_MAP.md`, `ARCHITECTURE.md`, `CONVENTIONS.md`, `CURRENT_STATE.md`, and `DECISIONS.md`. It records manifest, directory, configuration, Git, and recognized indexer evidence. Architecture boundaries and design decisions are explicitly left unknown unless source evidence supports them.
