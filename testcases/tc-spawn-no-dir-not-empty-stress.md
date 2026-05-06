Prerequisites:
  - From the worktree root:
    `/Users/fred.zhang/Projects/j`

Steps:
  - Run:
      go test ./internal/cli/tasks/... -run TestRunStart_NoFromFile_PicksTask -count=100

Expected:
  - Exit code 0.
  - All 100 iterations pass with no "TempDir RemoveAll cleanup: directory not
    empty" failure (or any other failure).
  - This test previously failed intermittently because `emitChildExit` reopened
    `agent.log` via `agentlog.EmitTo` after `os.RemoveAll` had already walked
    the task directory; the fix passes the already-open fd to the goroutine so
    no new directory entry is created.
