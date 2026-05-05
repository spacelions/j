Prerequisites:
  - From the worktree root:
    `/Users/fred.zhang/Projects/j`

Steps:
  - Run:
      go test ./internal/util/run/... -run TestSpawn_AppendsChildExitMarker -v

Expected:
  - Exit code 0.
  - `PASS: TestSpawn_AppendsChildExitMarker` is printed.
  - The test confirms that after a `Spawn`-ed child exits, a
    `>>> J {"event":"child_exit",...}` line with the correct `exit_code` and
    `pid` fields appears in `agent.log`. This invariant must be preserved even
    though `emitChildExit` now writes through the open fd (`agentlog.Emit`)
    rather than reopening by path (`agentlog.EmitTo`).
