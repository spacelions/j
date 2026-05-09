package testcases_test

import (
	"bytes"
	"errors"
	"os"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestOrchestrateFailFastOnContention verifies AC4: when RunOrchestrate
// finds the per-task flock already held, it exits non-zero and the
// returned error carries the holder's pid, host, phase, and start time.
// The DangerousDialogBox call must also produce non-empty stderr.
func TestOrchestrateFailFastOnContention(t *testing.T) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, nil)

	// Hold the lock in this process (different open-file-description,
	// same process — per-OFD flock semantics give us contention).
	holdCtx := tasks.WithPhase(t.Context(), "verifying")
	held, err := tasks.AcquireLock(holdCtx, id)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	t.Cleanup(func() { _ = held.Release() })

	var stderr bytes.Buffer
	runErr := clitasks.RunOrchestrate(
		t.Context(), clitasks.OrchestrateOptions{
			TaskID: id,
			Stderr: &stderr,
			Agents: []codingagents.Agent{
				testutil.NewScriptedAgent(),
			},
		},
	)
	if runErr == nil {
		t.Fatal("AC4: want error when lock is held, got nil")
	}

	// AC4: error must carry pid, host, phase, and start time.
	var locked *tasks.LockedError
	if !errors.As(runErr, &locked) {
		t.Fatalf("AC4: want *LockedError; got %T: %v", runErr, runErr)
	}
	if locked.Holder.PID != os.Getpid() {
		t.Errorf("AC4: holder pid=%d want %d",
			locked.Holder.PID, os.Getpid())
	}
	host, _ := os.Hostname()
	if locked.Holder.Host != host {
		t.Errorf("AC4: holder host=%q want %q",
			locked.Holder.Host, host)
	}
	if locked.Holder.Phase != "verifying" {
		t.Errorf("AC4: holder phase=%q want \"verifying\"",
			locked.Holder.Phase)
	}
	if locked.Holder.StartedAt.IsZero() {
		t.Error("AC4: holder started_at is zero")
	}
	// DangerousDialogBox must have been called with the message.
	if stderr.Len() == 0 {
		t.Error("AC4: stderr empty; contention message not written")
	}
}
