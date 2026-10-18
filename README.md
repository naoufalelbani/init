# init

`ai-init` is a cross-platform Go CLI for adding a careful, repository-aware foundation for AI coding tools. It inspects an existing project and creates editable project instructions and a concise project map. It never changes the project's language or architecture.

## Status

This project is an early, usable foundation. Setup, deterministic stack detection, safe managed `AGENTS.md` generation, project map generation, doctor, prompt display, and basic Git worktree lifecycle commands are implemented. Agent-specific adapters, transactional integration, port allocation, indexer integrations, and broad framework detection are not yet implemented. The CLI reports the integration limitation instead of claiming completion.

## Install

Requires Go 1.23+ to build. Prebuilt releases will be published through GitHub Releases when release automation is enabled.

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

`setup` preserves an existing custom `AGENTS.md`. Generated managed files are updated atomically, with a `.ai-init.bak` backup before replacing an existing file. Use `--dry-run` to preview reported file actions.

```sh
ai-init setup --path "../my project" --dry-run
ai-init setup --with agents,memory,worktrees
ai-init agents audit
ai-init memory update
```

## Commands

- `setup`: analyze a repository and generate selected artifacts.
- `analyze`: print detected stack, directories, and build/test hints.
- `agents generate|audit|sync|validate`: create managed instructions or inspect existing instructions. Custom files are preserved.
- `memory generate|update`: write `.ai/PROJECT_MAP.md` from detected evidence.
- `wt new <task>`: create a `codex/<task>` branch in a sibling worktree directory.
- `wt list|status <task>|sync <task>|cleanup <task>`: inspect, sync a clean task worktree, and safely remove clean worktrees. Cleanup refuses uncommitted changes or a branch not integrated into the current branch.
- `prompt agents|worktrees|audit|config`: display a reusable prompt.
- `doctor`, `version`, `help`.

## Agent configuration

The root `AGENTS.md` is the canonical contract for tools that read it, including Codex and OpenCode workflows configured to honor it. This release does not write IDE-specific configuration for Antigravity, Claude Code, Cursor, Windsurf, Cline, Aider, or GitHub Copilot. Check each product's current documentation before adding native configuration; unsupported adapters are intentionally not generated.

## Worktree workflow

```sh
ai-init wt new api-endpoint
ai-init wt new ui-form
ai-init wt new docs
ai-init wt list
```

Assign one independent task to each worktree. Each receives `.agent-task.md` with its branch and workspace. Once a task is committed, inspect and validate its branch before integration. Transactional combined-result integration is not yet provided; perform the merge manually in a clean target checkout and run the project's verification commands. `wt sync` merges the current branch into a clean task worktree and leaves ordinary Git conflict recovery available there.

## Safety

- Git is invoked with argument arrays, never a constructed shell command.
- Existing custom root instructions are preserved.
- Generated updates use a temporary file and rename, and retain a backup on replacement.
- Worktree cleanup checks for uncommitted work and whether the task branch is integrated.
- Generated project maps contain repository metadata only; review them before sharing.

## Contributing

Use focused changes, Go formatting, and standard library APIs where practical. Run `gofmt -w` on changed Go files and `go test ./...`. Add tests for new behavior using temporary repositories and temporary directories. Keep support claims aligned with implemented and verified behavior.
