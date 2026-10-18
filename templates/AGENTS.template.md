# Development rules for AI coding agents

These rules apply to every task in this repository. Project facts appear at the end and must be evidence-based.

## Architecture and boundaries
- Respect the existing architecture and identify module responsibilities before editing.
- Follow the project’s established framework, language, and platform conventions.
- Maintain separation of concerns and keep changes within the responsible module.
- Do not impose an architectural rewrite or change the project’s language without explicit direction.
- Trace affected callers, data flow, and external interfaces before changing behavior.
- Preserve public APIs and backward compatibility unless the task authorizes a breaking change.

## Reuse and duplication
- Search for an existing implementation before adding functionality.
- Reuse suitable utilities, components, and dependencies already present in the project.
- Avoid competing implementations of the same behavior.
- Prefer a direct implementation over an abstraction used only once.
- Introduce shared abstractions when repeated behavior has a clear common contract.

## Code quality
- Handle errors explicitly and provide enough context to diagnose failures.
- Avoid silent failures, temporary production stubs, dead code, and unnecessary dependencies.
- Keep functions and modules focused, understandable, and consistent with existing naming.
- Follow established formatting, linting, and type-checking rules.
- Avoid broad refactors mixed into a focused behavior change.
- Update relevant documentation when behavior, configuration, or interfaces change.

## Safety and data handling
- Preserve user changes and unrelated files.
- Never overwrite uncommitted work or run destructive Git commands without explicit authorization.
- Never remove functionality merely to make a test pass.
- Do not expose credentials, tokens, private keys, personal data, or other secrets.
- Do not copy environment files or secrets between worktrees.
- Keep generated context free of secrets and review it before committing or sharing.
- Do not mutate shared databases or services without explicit authorization.
- Keep task-local notes out of commits unless the user asks to track them.

## Development workflow
- Read project documentation and relevant instructions before changing code.
- Inspect affected files, callers, tests, and dependencies before editing.
- Make focused changes that address the requested behavior.
- Use existing build, test, formatting, and lint commands verified in this repository.
- Run relevant checks after changes and fix regressions caused by the task.
- Do not invent commands or claim checks passed when they were not run.
- Report what changed, what verification ran, and unresolved issues honestly.
- If verification cannot run, explain the blocker and provide the next useful check.

## Context efficiency
- Start with project documentation and relevant indexes when available.
- Prefer scoped searches and relevant files over dumping the whole repository.
- Use language servers, symbol indexes, Graphify, Serena, or MCP indexes when available.
- Treat index results as potentially stale and verify important claims against source.
- Record concise, durable project context rather than copying large source files into notes.
- Distinguish verified facts from generated summaries, assumptions, and unknowns.

## Multi-agent development
- Work only in the assigned workspace or Git worktree.
- Never edit another agent’s worktree or task-local files.
- Respect task boundaries and avoid shared mutable development state.
- Coordinate changes to shared interfaces before depending on them.
- Keep each parallel task on its dedicated branch and worktree.
- Review the combined result and run relevant checks before integration.
- Report branch names, changed files, and verification results when handing off.

## Project-specific facts
- Project: {{PROJECT_NAME}}
- Detected languages: {{LANGUAGES}}
- Detected frameworks: {{FRAMEWORKS}}
- Source directories: {{SOURCES}}
- Entry points: {{ENTRY_POINTS}}
- Git repository: {{GIT}}
- Build commands: {{BUILD_COMMANDS}}
- Test commands: {{TEST_COMMANDS}}
- Unknown facts must remain marked as unknown until verified.

## Dependencies and compatibility
- Prefer the project's existing dependency manager and lockfile.
- Add a dependency only when it materially reduces complexity or risk.
- Avoid replacing tools or versions as part of an unrelated task.
- Consider supported operating systems, architectures, and runtime versions.
- Keep configuration examples valid for the versions detected in the repository.
- Explain unavoidable compatibility changes in the task summary.

## Testing and verification
- Find the narrowest relevant checks first, then run broader checks when appropriate.
- Add or update tests for changed behavior and regression cases.
- Keep integration tests isolated from user data and shared external services.
- Do not weaken assertions or delete tests to make a change pass.
- Treat formatter, linter, type checker, build, and test results as separate evidence.
- Verify generated files and configuration with the target tool when practical.
- State clearly when a check was skipped, unavailable, or inconclusive.

## Project context and indexing
- Prefer concise maps of files, responsibilities, commands, and boundaries.
- Preserve manually maintained decisions and distinguish them from generated summaries.
- Do not assume an index or graph is current; verify relevant source before editing.
- Do not create duplicate documentation that restates existing project guides.
- Never include private repository content in external indexing services without approval.

## Change review
- Review the final diff for unrelated edits, generated artifacts, and accidental secrets.
- Check error paths and cleanup behavior for operations that create temporary state.
- Consider interrupted commands and concurrent work when changing shared files or metadata.
- Keep rollback and recovery steps available for changes that touch user data.
- Summarize any behavior that requires a manual follow-up from the user.

## Uncertainty
- Mark undetected commands, conventions, or architecture details as unknown.
- Ask for clarification when multiple interpretations would change data or public behavior.
- Prefer a reversible, focused change when a routine implementation detail is uncertain.
- Do not claim compatibility based only on a successful compile on one operating system.
