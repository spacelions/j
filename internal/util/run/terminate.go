package run

import (
	"context"
	"errors"
	"fmt"
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
) (terminated bool, err error) {
	if pid <= 0 || !IsAlive(pid) {
		return false, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, fmt.Errorf("terminate: find pid %d: %w", pid, err)
	}
	if err := signalAllowingDone(proc, syscall.SIGTERM); err != nil {
		return false, fmt.Errorf(
			"terminate: sigterm pid %d: %w", pid, err)
	}
	graceCtx, cancel := context.WithTimeout(ctx, grace)
	defer cancel()
	if waitErr := WaitForExit(graceCtx, pid); waitErr == nil {
		return true, nil
	}
	if !IsAlive(pid) {
		return true, nil
	}
	if err := signalAllowingDone(proc, syscall.SIGKILL); err != nil {
		return true, fmt.Errorf(
			"terminate: sigkill pid %d: %w", pid, err)
	}
	killCtx, killCancel := context.WithTimeout(ctx, grace)
	defer killCancel()
	if waitErr := WaitForExit(killCtx, pid); waitErr != nil {
		return true, fmt.Errorf(
			"terminate: pid %d alive after sigkill: %w", pid, waitErr)
	}
	return true, nil
}

// signalAllowingDone treats os.ErrProcessDone / ESRCH as success
// because the caller's intent ("send sig X") is satisfied by the
// process already being gone. EPERM (cross-user signalling) is
// surfaced so callers can decline a takeover instead of looping.
func signalAllowingDone(proc *os.Process, sig syscall.Signal) error {
	err := proc.Signal(sig)
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrProcessDone) ||
		errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
