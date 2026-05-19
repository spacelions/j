package testcases_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestSPA94ResumeWorkEmptyStateMessage pins acceptance criteria AC3:
// when no task carries a populated worker resume session, the
// resume-work command prints the exact, user-facing line
// "J: no tasks with an active work session". The wording is part of
// the contract because tooling, screenshots, and scripts grep on it.
//
// Black-box: seed a working-status row WITHOUT a worker resume
// session and call clitasks.RunResumeWork. The picker must be
// short-circuited (so the test does not have to wire a real picker
// UI) and the empty-state message must hit stdout verbatim.
func TestSPA94ResumeWorkEmptyStateMessage(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusWorking
		task.WorkResumeSession = ""
	})

	var stdout bytes.Buffer
	if err := clitasks.RunResumeWork(t.Context(), clitasks.ResumeWorkOptions{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{},
		UI:     spa94NoopUI{},
	}); err != nil {
		t.Fatalf("RunResumeWork: %v", err)
	}

	const want = "J: no tasks with an active work session"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want substring %q", stdout.String(), want)
	}
}

// spa94NoopUI satisfies clitasks.UI without driving any interactive
// prompt. The empty-state path returns before any picker is invoked,
// so PickTask must never run.
type spa94NoopUI struct{}

func (spa94NoopUI) PickTask(
	context.Context, []tasks.Task,
) (string, bool, error) {
	return "", false, nil
}

func (spa94NoopUI) ConfirmDiscard(
	context.Context, tasks.Task,
) (bool, error) {
	return false, nil
}
