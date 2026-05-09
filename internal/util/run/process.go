package run

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

// waitForExitPollInterval is the polling cadence WaitForExit uses to
// re-check IsAlive. 100ms is fast enough that the verify orchestrator
// barely notices the wait when a real cursor-agent / claude turn
// finishes (tens of seconds), and slow enough not to burn CPU on a
// tight loop while the child is mid-write.
const waitForExitPollInterval = 100 * time.Millisecond

// WaitForExit blocks until the OS process identified by pid is no
// longer alive, returning nil. It is intended for verify-loop
// synchronisation against Spawn-ed children: Spawn does not expose a
// Wait handle to the caller (a background goroutine inside Spawn does
// the wait4 reap so the kernel does not keep zombies around), so the
// orchestrator polls IsAlive instead and reads the child's findings
// file only after this returns.
func WaitForExit(ctx context.Context, pid int) error {
	if pid <= 0 {
		return nil
	}
	if !IsAlive(pid) {
		return nil
	}
	ticker := time.NewTicker(waitForExitPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if !IsAlive(pid) {
				return nil
			}
		}
	}
}

// IsAlive reports whether the OS process identified by pid is still
// running. It uses os.FindProcess + signal 0 (the standard "no-op"
// liveness probe on POSIX): an ESRCH / os.ErrProcessDone error means
// the process is gone. Any other error is conservatively treated as
// "alive" so a transient permission error does not cause the reaper
// to declare a still-running child dead.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false
	}
	if errors.Is(err, syscall.ESRCH) {
		return false
	}
	// EPERM means the process exists but is owned by another user
	// (or we are not allowed to signal it). Treat as alive.
	return true
}
