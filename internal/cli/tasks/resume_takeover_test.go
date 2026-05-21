package tasks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
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
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
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
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
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
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
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
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
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

// TestWaitLockReleased_ContextCancelled covers the ctx.Done() branch
// in the polling loop.
func TestWaitLockReleased_ContextCancelled(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, nil)
	// Hold the lock for the duration of the test.
	held, err := tasks.AcquireLock(t.Context(), id)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = held.Release() }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = waitLockReleased(ctx, id, os.Getpid())
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// TestTakeoverIfHeld_NonLockedError covers the !errors.As path:
// when AcquireLock returns a non-LockedError (e.g., EnsureDir fails
// because the .j/tasks dir is gone), the error is propagated directly.
func TestTakeoverIfHeld_NonLockedError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	setupContinueEnv(t)
	// Make the tasks directory inaccessible so EnsureDir fails.
	tasksDir := tasks.DefaultDir()
	if err := os.Chmod(tasksDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tasksDir, 0o755) })
	var stderr bytes.Buffer
	err := takeoverIfHeld(t.Context(), &stderr, "any-id")
	if err == nil {
		t.Fatal("expected error when tasks dir is inaccessible")
	}
}

// TestWaitLockReleased_TryAcquireError covers the TryAcquireForReap error
// path in waitLockReleased by making the task directory inaccessible so
// TryAcquireForReap's stat call fails.
func TestWaitLockReleased_TryAcquireError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, nil)
	tasksDir := tasks.DefaultDir()
	taskDir := filepath.Join(tasksDir, id)
	// Create the lock file so TryAcquireForReap doesn't short-circuit on ErrNotExist.
	lockPath := filepath.Join(taskDir, tasks.LockFileName)
	if err := os.WriteFile(lockPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Make the task directory inaccessible.
	if err := os.Chmod(taskDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(taskDir, 0o755) })
	err := waitLockReleased(t.Context(), id, os.Getpid())
	if err == nil {
		t.Fatal("expected TryAcquireForReap error")
	}
}
