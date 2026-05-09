package tasks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
)

// withFakeTerminate overrides the package-level allowlist that
// resume_shared.go calls to signal a holder. Restores the production
// run.Terminate after the test so other tests do not see leakage.
func withFakeTerminate(
	t *testing.T,
	fn func(ctx context.Context, pid int, grace time.Duration) (bool, error),
) {
	t.Helper()
	prev := terminate
	terminate = fn
	t.Cleanup(func() { terminate = prev })
}

// TestRunResumePlan_NoContentionStaysSilent pins case (a) of the A6
// matrix: when nobody holds the per-task flock, resume runs without
// printing the takeover note and never invokes the signal helper.
func TestRunResumePlan_NoContentionStaysSilent(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	called := 0
	withFakeTerminate(t, func(
		_ context.Context, _ int, _ time.Duration,
	) (bool, error) {
		called++
		return false, nil
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	var stderr bytes.Buffer
	if err := RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  &stderr,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunResumePlan: %v", err)
	}
	if called != 0 {
		t.Fatalf("terminate called %d times, want 0", called)
	}
	if strings.Contains(stderr.String(), "taking over") {
		t.Fatalf("stderr unexpectedly mentions takeover: %q", stderr.String())
	}
	_ = readSpawnedArgv(t, argvPath)
}

// TestRunResumePlan_TakeoverWhenHeld pins case (b): a real holder
// owns the per-task flock; resume prints the takeover note, calls
// the (fake) terminator, waits for the lock to drain, and proceeds.
func TestRunResumePlan_TakeoverWhenHeld(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	holderCtx := tasks.WithPhase(t.Context(), "planning")
	held, err := tasks.AcquireLock(holderCtx, id)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			_ = held.Release()
		}
	})
	withFakeTerminate(t, func(
		_ context.Context, _ int, _ time.Duration,
	) (bool, error) {
		_ = held.Release()
		released = true
		return true, nil
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	var stderr bytes.Buffer
	if err := RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  &stderr,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunResumePlan: %v", err)
	}
	if !strings.Contains(stderr.String(), "taking over task "+id) {
		t.Fatalf("stderr missing takeover note: %q", stderr.String())
	}
	_ = readSpawnedArgv(t, argvPath)
}

// TestRunResumePlan_TerminatorErrorBlocksSpawn pins case (d): when
// the terminator returns an error the resume command bubbles it up
// and never re-execs the orchestrator.
func TestRunResumePlan_TerminatorErrorBlocksSpawn(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	held, err := tasks.AcquireLock(t.Context(), id)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	t.Cleanup(func() { _ = held.Release() })
	withFakeTerminate(t, func(
		_ context.Context, _ int, _ time.Duration,
	) (bool, error) {
		return true, errors.New("permission denied")
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	err = RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cannot take over") {
		t.Fatalf("err = %v, want 'cannot take over'", err)
	}
}

// TestRunResumePlan_TerminatorSuccessButLockStillHeld pins case (e):
// terminator succeeds yet the lock is still held — resume waits for
// the grace window and then surfaces the "still alive" error.
func TestRunResumePlan_TerminatorSuccessButLockStillHeld(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	held, err := tasks.AcquireLock(t.Context(), id)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	t.Cleanup(func() { _ = held.Release() })
	withFakeTerminate(t, func(
		_ context.Context, _ int, _ time.Duration,
	) (bool, error) {
		return true, nil
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	err = RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "still alive") {
		t.Fatalf("err = %v, want 'still alive'", err)
	}
}
