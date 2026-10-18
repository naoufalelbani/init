# init

`ai-init` is a cross-platform Go CLI for adding a careful, repository-aware foundation for AI coding tools. It inspects an existing project and creates editable project instructions and a concise project map. It never changes the project's language or architecture.

## Status

The Go CLI, deterministic project analysis, managed `AGENTS.md` generation, documented agent adapters, persistent evidence-based context, port allocation, disposable-worktree integration, optional staged-content hooks, cautious `.gitattributes` setup, doctor, and reusable prompt library are implemented. Stack hints cover common Go, Rust, Dart/Flutter, Python, JavaScript/TypeScript, Java, C#/.NET, and CMake/Qt projects. Analysis records dependency-manager declarations and lockfiles, existing agent instruction paths, recognized formatting/lint configuration files, Git convention files, deployment/platform markers, and Graphify/Serena configuration paths. Project maps include a bounded source-path inventory from Git-tracked and non-ignored files, with a filesystem fallback; file contents are not indexed or copied. Indexers are not invoked by default; `--use-indexers` opts into documented local Serena and Graphify CLI operations when their existing project data is present, with filtered Graphify labels only. Database provisioning is explicit and currently limited to PostgreSQL URLs; other database engines remain manual. Detection is intentionally conservative: configuration contents remain authoritative, and it does not infer full UI/backend boundaries.

## Install

Prebuilt archives for Linux, macOS, and Windows (amd64 and arm64) are published by the release workflow when a `v*` version tag is pushed. Download the archive for your OS and architecture from [GitHub Releases](https://github.com/naoufalelbani/init/releases); the release includes `checksums.txt` for artifact verification. No Go installation is needed to use a downloaded binary. No versioned release has been tagged yet.

```sh
go install github.com/naoufalelbani/init/cmd/ai-init@latest
```

Or build locally:

```sh
git clone https://github.com/naoufalelbani/init.git
cd ai-init
go build -o ai-init ./cmd/ai-init
```

## Quick start

```sh
cd existing-project
ai-init setup
ai-init analyze
ai-init doctor
```

`setup` preserves an existing custom `AGENTS.md`. Generated managed files are updated atomically, with a `.ai-init.bak` backup before replacing an existing file. Existing unmarked agent configuration is preserved. Use `--dry-run` to preview file actions.

```sh
ai-init setup --path "../my project" --dry-run
ai-init setup --diff
ai-init setup --with agents,memory,worktrees
ai-init setup --with agents,memory --use-indexers
ai-init setup --with gitattributes
ai-init setup --with hooks
ai-init setup --adapters claude,cursor,copilot,aider
ai-init agents audit
ai-init agents sync --diff
ai-init agents list
ai-init memory update
ai-init memory update --use-indexers
```

## Commands

- `setup`: analyze a repository and generate selected artifacts.
- `analyze`: print detected stack, directories, and build/test hints.
- `agents generate|audit|sync|validate|configure|list`: create managed instructions, configure adapters, or inspect support. Custom files are preserved.
- `memory generate|update [--use-indexers]`: write `.ai/PROJECT_MAP.md`, `ARCHITECTURE.md`, `CONVENTIONS.md`, `CURRENT_STATE.md`, and `DECISIONS.md` from detected evidence. Managed sections refresh while manual text outside those markers is retained. Indexers are not called by default. With `--use-indexers`, an existing `.serena/project.yml` permits a Serena index refresh, and an existing `graphify-out/graph.json` permits a Graphify god-nodes query. No tool is installed; missing or failed indexers fall back to the deterministic scan. Graphify output is reduced to a few validated structural labels, with raw command output discarded. Dry runs never invoke indexers.
- `wt new <task> [--ide ADAPTER] [--port SERVICE] [--env NAME] [--isolate-postgres-env NAME --postgres-admin-env NAME]`: create an `agent/<task>` branch in a sibling worktree directory and optionally configure instructions, allocate ports, propagate explicitly selected environment variables, or provision one task-local PostgreSQL database. Creation serializes concurrent ai-init processes; failed setup rolls back the newly created branch, worktree, successfully provisioned database, environment snapshot, and port allocations. If the client reports an ambiguous create failure, ai-init retains recovery metadata and does not issue an automatic drop. When `--ide` is supplied, ai-init configures that adapter and launches it after creation if a supported launcher is available.
- `wt open <task> --ide ADAPTER`: open a task worktree with Cursor, Codex, Claude Code, OpenCode, or Aider. Terminal agents run interactively in the worktree and keep the command attached until they exit. Antigravity, Cline, Copilot, and Windsurf launch commands are not implemented; those adapters still support instruction configuration.
- `wt list|status [task]|sync <task> [--dry-run]|integrate <task> [--dry-run]|cleanup <task> [--dry-run] [--drop-database --postgres-admin-env NAME]`: inspect, preview or sync, validate and integrate, or safely remove clean worktrees. Cleanup refuses uncommitted changes or a branch not integrated into the current branch. A task with a provisioned database also requires the explicit drop option and matching administrator endpoint.
- `ports allocate|list|release`: manage explicit, per-task service port records.
- `hooks check-staged`: manually run the optional pre-commit checks against the Git index.
- `prompt agents|worktrees|audit|config`: display a reusable prompt or export it with `--output FILE` (existing files are never overwritten).
- `setup --with gitattributes`: add managed cross-platform text rules only when `.gitattributes` is absent and the Git index has no tracked files. Existing custom files and tracked repositories are preserved; setup never renormalizes existing files.
- `setup --with hooks`: install a local pre-commit check only when Git is using its default hooks path, no hook already exists, and `ai-init` is on `PATH` for Git to invoke. `ai-init hooks check-staged` can also be run directly.
- `doctor [--path DIR]`: inspect the detected project and report whether required build/test executables and local wrappers are available. It does not run project commands and returns a failing status when a detected prerequisite is missing. It checks availability, not versions, dependencies, services, or build/test success.
- `version`, `help`.

## Agent configuration

The root `AGENTS.md` is canonical. Antigravity, Codex, OpenCode, and Cline read it natively. `ai-init agents configure --adapter NAME` can also create minimal documented references for Claude Code (`CLAUDE.md` imports `AGENTS.md`), Cursor (`.cursor/rules/ai-init.mdc`), Windsurf (`.windsurf/rules/ai-init.md`), GitHub Copilot (`.github/copilot-instructions.md`), or Aider (`.aider.conf.yml`). Setup detects existing adapter configuration and configures those; pass `--adapters` to choose explicitly. Existing unmarked files are preserved.

The adapter locations and formats follow vendor documentation: [Antigravity rules](https://www.antigravity.google/docs/rules/), [Claude Code memory files](https://docs.anthropic.com/en/docs/claude-code/memory), [Cursor rules](https://docs.cursor.com/context/rules), [Windsurf Cascade rules](https://docs.windsurf.com/windsurf/cascade/memories), [Cline rules](https://docs.cline.bot/customization/cline-rules), [Copilot repository instructions](https://docs.github.com/en/copilot/how-tos/copilot-on-github/customize-copilot/add-custom-instructions/add-repository-instructions), [Aider conventions](https://aider.chat/docs/usage/conventions.html), and [OpenCode rules](https://opencode.ai/docs/rules/).

## Worktree workflow

```sh
# Run these in separate terminals; each --ide session opens in its own worktree.
ai-init wt new api-endpoint --ide codex --port api
ai-init wt new ui-form --ide claude
ai-init wt new docs --ide opencode
# Explicit PostgreSQL isolation; ADMIN_DATABASE_URL is never copied to the worktree.
ai-init wt new isolated-api --isolate-postgres-env DATABASE_URL --postgres-admin-env ADMIN_DATABASE_URL
ai-init wt list
ai-init wt integrate api-endpoint
ai-init wt cleanup isolated-api --drop-database --postgres-admin-env ADMIN_DATABASE_URL
```

Use `ai-init wt open api-endpoint --ide codex` to reopen an existing task worktree later.

Each worktree receives an `.agent-task.md` primer. `wt integrate` requires clean source and target worktrees, locks concurrent ai-init integrations, combines changes in a disposable worktree, runs detected test commands (Go, Cargo, Dart/Flutter, pytest, Node package scripts, Maven/Gradle, and dotnet), and fast-forwards the target only after validation succeeds. Windows wrapper commands are run through `cmd.exe`; Unix wrappers are run directly. Conflicts and test failures leave both original branch heads unchanged. The lock does not stop unrelated Git commands, so branch heads and worktree cleanliness are checked again before the update. Projects with no detected test command are refused. `wt sync` merges the current branch into a clean task worktree and leaves ordinary Git conflict recovery available there.

Three agents can independently implement an API endpoint, a UI form, and documentation in separate worktrees. Integrate each task with `wt integrate <task>`, review the target diff, and run the full project checks before shipping.

## Safety

- Git is invoked with argument arrays, never a constructed shell command.
- Existing custom root instructions are preserved.
- Generated updates use a temporary file and rename, and retain a backup on replacement.
- Worktree cleanup checks for uncommitted work and whether the task branch is integrated.
- Generated project maps contain repository metadata only; review them before sharing.

## Contributing

Use focused changes, Go formatting, and standard library APIs where practical. Run `gofmt -w` on changed Go files and `go test ./...`. Add tests for new behavior using temporary repositories and temporary directories. Keep support claims aligned with implemented and verified behavior.

## Limitations

- `wt new --ide` launches supported adapters after creating and configuring the worktree; the worktree-creation lock is released before the interactive tool starts. If the adapter has no verified launcher or the executable is missing, creation remains complete and ai-init reports the workspace path and launch problem. Use `wt open <task> --ide <adapter>` to reopen a supported tool later. Launch commands are currently unavailable for Antigravity, Cline, GitHub Copilot, and Windsurf; their instruction adapters remain usable.
- Environment values are never copied by default. Each repeated `--env NAME` explicitly selects a variable from ai-init's process environment. Selected values are stored in the ignored `.ai-init.env.json` task-local file and named in the task primer; values are never printed. For PostgreSQL only, `--isolate-postgres-env NAME --postgres-admin-env NAME` explicitly provisions a unique task database on the same host and port, rewrites the selected application URL, and requires `createdb` and `dropdb` on `PATH`. The admin URL is used only in memory and a temporary PostgreSQL passfile; its variable is removed from the child client environment, and the URL is not stored in the snapshot. The URL parser intentionally accepts only one TCP host, simple ASCII role and maintenance-database names, and optional `sslmode`. Cleanup requires `--drop-database` and the matching admin endpoint after the worktree is clean and integrated. This does not provision a server or isolate other database engines. Treat environment snapshots as sensitive and do not commit them. On Windows the file inherits the containing directory's ACL; on Unix it is created with owner-only permissions.
- A free port is chosen and recorded, then its temporary socket is closed. The record is not a persistent socket reservation, and ai-init does not automatically inject environment variables into a service process.
- Graphify and Serena are detected by default but not invoked. Explicit `--use-indexers` opts into the documented local CLI interfaces for an existing Serena project configuration or conventional Graphify graph; failures use the deterministic fallback. Only filtered Graphify structural labels are persisted, and no indexer is installed.
- Setup does not propagate environment files or provision databases. The opt-in worktree PostgreSQL path does not support other engines or multi-host/Unix-socket URLs. `.gitattributes` support is explicit opt-in and is skipped when tracked files exist, because applying text attributes can make existing checkouts appear modified. Git hook checks are local safeguards, inspect staged additions only, and can be bypassed; existing/custom hook systems are preserved. Worktree environment propagation is opt-in by variable name; no `.env` files are copied.
- Existing unmarked agent configuration is preserved as-is; run the corresponding tool's own validator after manual edits.
- Static detection may miss monorepo packages, custom scripts, and project-specific test conventions; inspect `analyze` output before relying on generated hints.

## Optional service ports

Port allocation is explicit because ai-init cannot reliably infer which services a repository needs:

```sh
ai-init wt new api-task --port api --port web
ai-init ports list
ai-init ports release api-task
```

The allocator binds loopback to ask the operating system for an available port, closes that socket, and records the choice under `.ai-init/ports.json`. A directory lock serializes concurrent allocators. The reservation is metadata rather than a persistent listening socket, so another process can claim the port before the service starts; pass the reported value to the project's own configuration. Do not put secrets in the port registry.
