Prerequisites:
  - From the worktree root:
    `/Users/fred.zhang/Projects/j`

Steps:
  - Run:
      go test ./...

Expected:
  - Exit code 0.
  - Every package reports `ok` or `[no test files]` — no failures, no panics.
  - This ensures the SpawnIn fd-ownership fix does not regress any other package.
