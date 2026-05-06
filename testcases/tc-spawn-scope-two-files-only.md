Prerequisites:
  - From the worktree root:
    `/Users/fred.zhang/Projects/j`

Steps:
  - List the non-test Go files modified vs main:
      git diff --name-only main..HEAD -- '*.go' | rg -v '_test\.go$'
  - Verify neither file exceeds 300 lines:
      git diff --name-only main..HEAD -- '*.go' \
        | rg -v '_test\.go$' \
        | xargs wc -l

Expected:
  - Exactly two files are listed:
      internal/util/run/run.go
      internal/util/agentlog/agentlog.go
  - Neither file exceeds 300 lines (run.go ≤ 300, agentlog.go ≤ 300).
  - No test-only files (`*_test.go`) are modified.
