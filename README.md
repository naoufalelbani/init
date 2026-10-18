# init

`ai-init` is a cross-platform Go CLI for adding a careful, repository-aware foundation for AI coding tools. It inspects an existing project and creates editable project instructions and a concise project map. It never changes the project's language or architecture.

## Status

The Go CLI, deterministic project analysis, managed `AGENTS.md` generation, documented agent adapters, persistent evidence-based context, port allocation, disposable-worktree integration, doctor, and reusable prompt library are implemented. Stack hints cover common Go, Rust, Dart/Flutter, Python, JavaScript/TypeScript, Java, C#/.NET, and CMake/Qt projects. Graphify and Serena are detected as available context sources, but ai-init does not invoke their APIs. Detection is intentionally conservative and does not infer full UI/backend boundaries, lint conventions, or database isolation.

## Install

Requires Go 1.26+ to build. Prebuilt releases are published through GitHub Releases when a version tag is pushed.

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
ai-init setup --with agents,memory,worktrees
ai-init setup --adapters claude,cursor,copilot,aider
ai-init agents audit
ai-init agents list
ai-init memory update
```

## Commands

- `setup`: analyze a repository and generate selected artifacts.
- `analyze`: print detected stack, directories, and build/test hints.
- `agents generate|audit|sync|validate|configure|list`: create managed instructions, configure adapters, or inspect support. Custom files are preserved.
- `memory generate|update`: write `.ai/PROJECT_MAP.md`, `ARCHITECTURE.md`, `CONVENTIONS.md`, `CURRENT_STATE.md`, and `DECISIONS.md` from detected evidence. Managed sections refresh while manual text outside those markers is retained.
- `wt new <task> [--ide ADAPTER] [--port SERVICE]`: create an `agent/<task>` branch in a sibling worktree directory and optionally configure instructions/allocate ports. Creation serializes concurrent ai-init processes; failed setup rolls back the newly created branch, worktree, ignore entry, and port allocations. IDE windows are not launched automatically yet.
- `wt list|status [task]|sync <task> [--dry-run]|integrate <task> [--dry-run]|cleanup <task> [--dry-run]`: inspect, preview or sync, validate and integrate, or safely remove clean worktrees. Cleanup refuses uncommitted changes or a branch not integrated into the current branch.
- `ports allocate|list|release`: manage explicit, per-task service port records.
- `prompt agents|worktrees|audit|config`: display a reusable prompt or export it with `--output FILE` (existing files are never overwritten).
- `doctor`, `version`, `help`.

## Agent configuration

The root `AGENTS.md` is canonical. Antigravity, Codex, OpenCode, and Cline read it natively. `ai-init agents configure --adapter NAME` can also create minimal documented references for Claude Code (`CLAUDE.md` imports `AGENTS.md`), Cursor (`.cursor/rules/ai-init.mdc`), Windsurf (`.windsurf/rules/ai-init.md`), GitHub Copilot (`.github/copilot-instructions.md`), or Aider (`.aider.conf.yml`). Setup detects existing adapter configuration and configures those; pass `--adapters` to choose explicitly. Existing unmarked files are preserved.

The adapter locations and formats follow vendor documentation: [Antigravity rules](https://www.antigravity.google/docs/rules/), [Claude Code memory files](https://docs.anthropic.com/en/docs/claude-code/memory), [Cursor rules](https://docs.cursor.com/context/rules), [Windsurf Cascade rules](https://docs.windsurf.com/windsurf/cascade/memories), [Cline rules](https://docs.cline.bot/customization/cline-rules), [Copilot repository instructions](https://docs.github.com/en/copilot/how-tos/copilot-on-github/customize-copilot/add-custom-instructions/add-repository-instructions), [Aider conventions](https://aider.chat/docs/usage/conventions.html), and [OpenCode rules](https://opencode.ai/docs/rules/).

## Worktree workflow

```sh
ai-init wt new api-endpoint --ide antigravity --port api
ai-init wt new ui-form
ai-init wt new docs
ai-init wt list
ai-init wt integrate api-endpoint
```

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

- IDE launch is not implemented; `--ide` prepares supported instruction files and prints the workspace path.
- A free port is chosen and recorded, then its temporary socket is closed. The record is not a persistent socket reservation, and ai-init does not automatically inject environment variables into a service process.
- Graphify and Serena are detected but not called through their tool interfaces.
- Setup does not propagate environment files, install Git hooks, generate `.gitattributes`, or isolate databases. It never copies secrets automatically.
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
