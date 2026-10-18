# Worktrees

`wt new` creates a sibling `<repo>-worktrees/<task>` checkout on `codex/<task>`. `wt sync` merges the current branch into a clean task checkout. `wt cleanup` refuses dirty or unintegrated work. Transactional integration and automatic combined-result tests are not yet implemented; review and merge manually.
