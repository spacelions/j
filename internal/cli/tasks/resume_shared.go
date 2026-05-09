package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/util/run"
)

// takeoverGrace is the wall-clock window the resume-* / re-* takeover
// gives the previous holder to react to a forwarded SIGTERM before
// escalating to SIGKILL. Two seconds matches run.TerminateGrace and
// keeps an interactive "take over" prompt feeling responsive.
const takeoverGrace = 2 * time.Second

// terminate is a package-level allowlist override for run.Terminate
// so resume-* unit tests can drive the takeover branches without
// shelling out to a real `sleep` child. Production callers leave it
// at the default.
var terminate = run.Terminate

// resumeOptions is the common option set for all resume-* commands.
type resumeOptions struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	UI      UI
	JBinary string
}

func (o resumeOptions) withDefaults() resumeOptions {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.UI == nil {
		o.UI = newHuhUI(o.Stdin, o.Stderr)
	}
	return o
}

// resumePhaseConfig captures what differs between resume-plan,
// resume-work, and resume-verify.
type resumePhaseConfig struct {
	emptyMsg        string
	resumeEvent     tasks.Event
	errorVerb       string
	hasSession      func(tasks.Task) bool
	orchestrateArgs func(taskID string) []string
}

// runResumePhase is the shared implementation for resume-plan,
// resume-work, and resume-verify.
func runResumePhase(
	ctx context.Context, opts resumeOptions, cfg resumePhaseConfig,
) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()

	taskID, ok, err := pickResumeTaskID(ctx, opts, cfg)
	if err != nil || !ok {
		return err
	}
	t, err := resolver.TaskByID(taskID)
	if err != nil {
		return err
	}
	if !tasks.IsLegal(t.Status, cfg.resumeEvent) {
		return fmt.Errorf("cannot %s task in status %q",
			cfg.errorVerb, t.Status)
	}
	if _, err := tasks.EnsureDir(taskID); err != nil {
		return err
	}
	if err := takeoverIfHeld(ctx, opts.Stderr, taskID); err != nil {
		return err
	}
	return runInlineOrchestrator(ctx, opts.JBinary, cfg.orchestrateArgs(taskID))
}

// takeoverIfHeld signals the previous holder of the per-task lock so
// the orchestrate child this command is about to re-exec can acquire
// cleanly. Probes the lock via AcquireLock. On LockedError it prints a
// stderr takeover note, calls run.Terminate (SIGTERM, then SIGKILL on
// stubborn holders), and waits for the kernel-level lock to drain via
// repeated TryAcquireForReap probes. Returns nil if there was no
// contention — the legality check ran first so the typical
// "no-contention resume" path stays silent. Returns a wrapped error
// when the holder survives both signals so callers do not spawn into
// a still-locked task.
func takeoverIfHeld(
	ctx context.Context, stderr io.Writer, taskID string,
) error {
	probe, err := tasks.AcquireLock(ctx, taskID)
	if err == nil {
		_ = probe.Release()
		return nil
	}
	var locked *tasks.LockedError
	if !errors.As(err, &locked) {
		return err
	}
	uitheme.NormalFprintf(stderr,
		"J: taking over task %s from pid %d (was in %s)\n",
		taskID, locked.Holder.PID, locked.Holder.Phase)
	if _, termErr := terminate(
		ctx, locked.Holder.PID, takeoverGrace,
	); termErr != nil {
		return fmt.Errorf("cannot take over task %s: %w", taskID, termErr)
	}
	return waitLockReleased(ctx, taskID, locked.Holder.PID)
}

// waitLockReleased polls TryAcquireForReap until the previous holder
// has dropped its kernel flock, then immediately releases the probe.
// Designed for the post-Terminate window where the kernel has begun
// reaping the holder but the Release() in the holder's defer has not
// yet returned. The poll cadence is the same as WaitForExit's
// (100ms); the deadline is two takeoverGrace periods because in
// practice the SIGKILL escalation already consumed one grace.
func waitLockReleased(
	ctx context.Context, taskID string, prevPID int,
) error {
	deadline := time.Now().Add(2 * takeoverGrace)
	for time.Now().Before(deadline) {
		probe, err := tasks.TryAcquireForReap(taskID)
		if err != nil {
			return err
		}
		if probe != nil {
			_ = probe.Release()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf(
		"cannot take over task %s (pid %d still alive)",
		taskID, prevPID)
}

func pickResumeTaskID(
	ctx context.Context, opts resumeOptions, cfg resumePhaseConfig,
) (string, bool, error) {
	s, err := tasks.OpenDefault()
	if err != nil {
		return "", false, err
	}
	rows, err := s.ListTasks()
	_ = s.Close()
	if err != nil {
		return "", false, err
	}
	filtered := filterTasksBySession(rows, cfg.hasSession)
	if len(filtered) == 0 {
		uitheme.NormalFprintln(opts.Stdout, cfg.emptyMsg)
		return "", false, nil
	}
	tasks.SortTasks(filtered)
	return opts.UI.PickTask(ctx, filtered)
}

func filterTasksBySession(
	rows []tasks.Task, hasSession func(tasks.Task) bool,
) []tasks.Task {
	out := make([]tasks.Task, 0, len(rows))
	for _, t := range rows {
		if hasSession(t) {
			out = append(out, t)
		}
	}
	return out
}
