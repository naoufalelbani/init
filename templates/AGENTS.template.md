# Development rules for AI coding agents

## Architecture
- Respect the existing architecture and identify module responsibilities before editing.
- Follow established framework conventions and boundaries.
- Do not impose an architectural rewrite or change the project's language without explicit direction.

## Reuse
- Search for an existing implementation before adding functionality.
- Reuse suitable utilities and components; avoid competing implementations and unnecessary abstractions.
- Preserve backward compatibility unless the task authorizes a breaking change.

## Code quality
- Handle errors explicitly and avoid silent failures.
- Do not add temporary production stubs, dead code, or unnecessary dependencies.
- Keep changes modular, readable, and consistent with project naming and formatting.

## Safety
- Preserve user changes and unrelated files.
- Never overwrite uncommitted work or run destructive Git commands without explicit authorization.
- Never remove functionality merely to make a check pass.
- Do not expose credentials, tokens, private keys, or other secrets.
- Keep generated context free of secrets and review it before committing.

## Workflow
- Read project documentation and relevant instructions first.
- Inspect affected code, callers, and dependencies before editing.
- Make focused changes and verify with commands supported by this repository.
- Fix regressions caused by the change and report unresolved issues honestly.
- Use scoped searches; avoid dumping the entire repository when targeted inspection works.
- Treat index results as potentially stale and verify important details against source.

## Multi-agent work
- Work only in the assigned workspace or Git worktree.
- Never edit another agent's worktree or task-local files.
- Respect task boundaries and avoid shared mutable development state.
- Review the combined result and run relevant checks before integration.

## Project-specific facts
<!-- Replace this section with evidence gathered from this repository. Mark unknown details as unknown. -->
- Languages/frameworks: unknown
- Build and test commands: unknown
- Source directories: unknown
