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

// TestCodeReviewRejectsConcurrentRun verifies that a code-review run
// fails before fetch/planner work when the task flock is already held.
func TestCodeReviewRejectsConcurrentRun(t *testing.T) {
	freshInit(t)
	id := testutil.SeedFullTask(t, func(row *tasks.Task) {
		row.Status = tasks.StatusWorkDone
		row.PullRequestURL = "https://github.com/acme/app/pull/42"
	})

	held, err := tasks.AcquireLock(
		tasks.WithPhase(t.Context(), "code-review"), id)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	t.Cleanup(func() { _ = held.Release() })

	var stderr bytes.Buffer
	runErr := clitasks.RunCodeReviewChild(
		t.Context(), clitasks.CodeReviewChildOptions{
			TaskID: id,
			Stderr: &stderr,
			Agents: []codingagents.Agent{
				testutil.NewScriptedAgent(),
			},
		},
	)
	if runErr == nil {
		t.Fatal("want lock contention error, got nil")
	}

	var locked *tasks.LockedError
	if !errors.As(runErr, &locked) {
		t.Fatalf("want *LockedError, got %T: %v", runErr, runErr)
	}
	if locked.Holder.PID != os.Getpid() {
		t.Fatalf("holder pid = %d, want %d",
			locked.Holder.PID, os.Getpid())
	}
	if locked.Holder.Phase != "code-review" {
		t.Fatalf("holder phase = %q, want code-review",
			locked.Holder.Phase)
	}
	if stderr.Len() == 0 {
		t.Fatal("contention did not render dangerous stderr output")
	}
}
