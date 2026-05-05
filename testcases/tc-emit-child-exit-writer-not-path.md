Prerequisites:
  - From the worktree root:
    `/Users/fred.zhang/Projects/j`

Steps:
  - Confirm `emitChildExit` accepts an `io.Writer` (not a path string):
      grep -n 'func emitChildExit' internal/util/run/run.go
  - Confirm the body calls `agentlog.Emit` (not `agentlog.EmitTo`):
      grep -n 'agentlog\.' internal/util/run/run.go
  - Confirm the `defer logFile.Close()` is removed from `SpawnIn`:
      grep -n 'defer.*logFile.Close' internal/util/run/run.go
  - Confirm the goroutine closes logFile and calls emitChildExit with the fd:
      grep -n 'logFile' internal/util/run/run.go

Expected:
  - `func emitChildExit` signature contains `w io.Writer` (no `logPath string`).
  - The only `agentlog.` call in run.go is `agentlog.Emit(w, ...)`, not `agentlog.EmitTo`.
  - `grep -n 'defer.*logFile.Close'` returns no matches (defer removed).
  - The goroutine body contains `emitChildExit(logFile, ...)` and `logFile.Close()`.
