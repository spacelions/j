Prerequisites:
  - From the worktree root.

Steps:
  - List every Go non-test file modified or added by this branch (relative
    to `main`) and check no line count exceeds 300:
      git diff --name-only main..HEAD -- '*.go' \
        | rg -v '_test\.go$' \
        | xargs -I{} wc -l {} \
        | awk '$1 > 300 {print}'

Expected:
  - The output is empty (no modified/added non-test file exceeds the
    300-line cap).
  - Pre-existing files that were already over the cap on `main` (e.g.
    `internal/store/store.go` at 435 lines, `internal/cli/tasks/continue.go`
    at 410 lines, `internal/coding-agents/cursor/cursor.go` at 315 lines)
    are not touched by this branch and therefore are out of scope.
