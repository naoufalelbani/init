# Worktrees

`wt new <task>` creates `agent/<task>` in a sibling `<repo>-worktrees/<task>` checkout. It does not switch the primary checkout. It generates `.agent-task.md` and adds that filename to the repository's local Git exclude file when not already excluded. The primer is ignored locally; the task's code/config changes remain ordinary branch changes.

`wt sync <task>` requires a clean task worktree and merges the currently checked-out target branch into it. Git conflicts remain in that task worktree for ordinary resolution.

`wt integrate <task>` requires clean source and target worktrees and named branches. It creates a lock directory under Git's metadata path, captures target and task commit IDs, adds a detached disposable worktree at the target commit, merges the exact task commit, then runs detected Go or Cargo test commands there. Before the update it verifies the target head, target branch, task head, and both worktree cleanliness again. Only then does it run a fast-forward merge of the tested task commit into the target. The task branch is not rebased, reset, or discarded.

On conflict or validation failure, the target and task commit IDs are unchanged. The disposable worktree is aborted/removed by deferred cleanup. If cleanup itself fails, ai-init prints the path for manual recovery. A leftover lock directory is never auto-deleted; inspect active processes before removing it. The lock coordinates ai-init integrations, not arbitrary Git commands, so the final state checks protect against some external races and the fast-forward requirement rejects divergent target updates.

Only detected test commands are run. Current detection covers Go (`go test ./...`) and Cargo (`cargo test`). Projects without a detected test command are not integrated automatically. Run broader project checks after integration.

Cleanup requires a clean worktree whose branch is an ancestor of the current target branch. There is no force option that discards work.
