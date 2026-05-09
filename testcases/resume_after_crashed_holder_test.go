package testcases_test

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

const (
	lockCrashHolderKey = "J_LOCK_CRASH_HOLDER"
	lockCrashPathKey   = "J_LOCK_CRASH_PATH"
)

// TestResumeAfterCrashedHolder verifies AC5 and AC9: after a
// lock-holding process is SIGKILL'd, RunResumePlan acquires the lock
// immediately — no --force flag, no manual unlock, no takeover message.
// The kernel releases the flock when the holder's process exits.
//
// When spawned as the lock-holding child (lockCrashHolderKey=1) this
// function acquires the lock at lockCrashPathKey and sleeps until
// SIGKILL terminates it.
func TestResumeAfterCrashedHolder(t *testing.T) {
	if os.Getenv(lockCrashHolderKey) == "1" {
		path := os.Getenv(lockCrashPathKey)
		if path == "" {
			fmt.Fprintln(os.Stderr, lockCrashPathKey+" not set")
			os.Exit(1)
		}
		lockHolderRunAt(path)
		return
	}
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-session"
	})
	taskDir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	lockPath := filepath.Join(taskDir, tasks.LockFileName)

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe,
		"-test.run", "^TestResumeAfterCrashedHolder$",
	)
	cmd.Env = append(os.Environ(),
		lockCrashHolderKey+"=1",
		lockCrashPathKey+"="+lockPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	// Wait for the child to hold the lock.
	lockWaitForHolder(t, lockPath)

	// Simulate a crash: SIGKILL cannot be caught or ignored.
	// The kernel releases the flock immediately when the process exits.
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL: %v", err)
	}
	// Reap the zombie so IsAlive probes in the flock path see the
	// process as dead and the process table entry is freed.
	_, _ = cmd.Process.Wait()

	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	var stderr strings.Builder
	err = clitasks.RunResumePlan(
		t.Context(), clitasks.ResumePlanOptions{
			Stdin:  strings.NewReader(""),
			Stdout: io.Discard,
			Stderr: &stderr,
			Agents: []codingagents.Agent{
				testutil.NewScriptedAgent(),
			},
			UI:      &recoveryFakeUI{pickReturn: id},
			JBinary: recoveryArgvJStub(t, argvPath),
		},
	)
	// AC9: must succeed without --force or manual unlock.
	if err != nil {
		t.Fatalf("AC9: RunResumePlan after crash: %v", err)
	}
	// AC5: no takeover note — the lock was already free.
	if strings.Contains(stderr.String(), "taking over") {
		t.Errorf(
			"AC5: unexpected takeover note after crash; "+
				"lock should be free; got %q",
			stderr.String(),
		)
	}
	// Stub orchestrator was invoked (lock drain worked).
	_ = recoveryReadStubArgv(t, argvPath)
}
