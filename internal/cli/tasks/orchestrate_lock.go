package tasks

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spacelions/j/internal/lifecycle/orchestrator"
	"github.com/spacelions/j/internal/store/tasks"
)

// phaseTagFor maps the RunPhase enum to the human-readable phase
// string written into the per-task lock file's holder metadata. The
// resume-* / re-* CLI wrappers thread their phase through this same
// enum so the contention message can name "planning" / "working" /
// "verifying" without the holder having to remember which command it
// came from.
func phaseTagFor(phase orchestrator.RunPhase) string {
	switch phase {
	case orchestrator.RunPhaseFromWork:
		return "working"
	case orchestrator.RunPhaseVerifyOnly:
		return "verifying"
	default:
		return "planning"
	}
}

// contentionMessage formats the friendly "task already in use" line
// printed to stderr when AcquireLock fails with *LockedError. Includes
// the holder pid, host, phase, and start timestamp so the user knows
// which `j tasks resume-*` to invoke for a takeover.
func contentionMessage(taskID string, h tasks.Holder) string {
	return fmt.Sprintf(
		"task %s is already in use by pid %d on %s "+
			"(phase: %s, started %s). Use j tasks resume-%s to take over.",
		taskID, h.PID, h.Host, h.Phase,
		h.StartedAt.Format("15:04:05"), takeoverSubcommand(h.Phase),
	)
}

func takeoverSubcommand(phase string) string {
	switch phase {
	case "working":
		return "work"
	case "verifying":
		return "verify"
	default:
		return cmdPlan
	}
}

// installOrchestrateSignalHandler wires SIGTERM/SIGINT to ctx
// cancellation. The exec package's cmd.Cancel hook (set by the
// `run` package) translates the cancel into a SIGTERM at the phase
// child, then escalates to SIGKILL after the configured grace, so the
// orchestrator's per-task flock truly releases on shutdown rather
// than lingering until kernel-level reap. signal.Stop is run via the
// returned cleanup so re-entries (tests) do not leak handlers.
func installOrchestrateSignalHandler(
	ctx context.Context,
) (context.Context, func()) {
	derived, cancel := context.WithCancel(ctx)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	done := make(chan struct{})
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-done:
		}
	}()
	return derived, func() {
		signal.Stop(sigCh)
		close(done)
		cancel()
	}
}
