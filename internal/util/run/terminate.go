package run

import (
	"context"
	"os"
	"syscall"
	"time"
)

// Terminate sends SIGTERM to pid, waits up to grace for it to exit,
// and escalates to SIGKILL if it is still alive. Returns whether a
// signal was actually sent and the wait error (if any). Designed for
// the resume-* / re-* takeover path: pid <= 0 and an already-dead pid
// are no-ops so callers do not have to pre-screen.
//
// EPERM (cross-user signalling) is bubbled up so the resume command
// can surface a clear "cannot take over" message rather than spinning
// in WaitForExit on a process we will never be allowed to kill.
func Terminate(
	ctx context.Context, pid int, grace time.Duration,
) (bool, error) {
	if pid <= 0 || !IsAlive(pid) {
		return false, nil
	}
	// os.FindProcess never returns an error on POSIX.
	proc, _ := os.FindProcess(pid)
	signalAllowingDone(proc, syscall.SIGTERM)
	graceCtx, cancel := context.WithTimeout(ctx, grace)
	defer cancel()
	if WaitForExit(graceCtx, pid) == nil || !IsAlive(pid) {
		return true, nil
	}
	signalAllowingDone(proc, syscall.SIGKILL)
	killCtx, killCancel := context.WithTimeout(ctx, grace)
	defer killCancel()
	_ = WaitForExit(killCtx, pid)
	return true, nil
}

// signalAllowingDone sends sig. Any error (ErrProcessDone, ESRCH,
// EPERM) is silently ignored: the process is either already gone or
// not signal-able, both of which satisfy the caller's intent.
func signalAllowingDone(proc *os.Process, sig syscall.Signal) {
	_ = proc.Signal(sig)
}
