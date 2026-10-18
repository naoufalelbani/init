# Security

ai-init runs local Git commands and writes generated files under the selected repository. It does not call cloud APIs. Review generated artifacts before committing or sharing. Do not place credentials in project context. Git hooks are not installed by this release. Worktree cleanup does not remove dirty or unintegrated branches.
