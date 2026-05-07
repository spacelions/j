package testcases_test

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	clitasks "github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestVerify_ResumeWork_SucceedsFromCompleted pins acceptance
// criterion 5 (completed half): resume-work must succeed for a
// completed row carrying a work resume session. Backed by the new
// `{completed, EventWorkResume, working}` FSM edge.
func TestVerify_ResumeWork_SucceedsFromCompleted(t *testing.T) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusCompleted
		task.WorkResumeSession = "sess-x"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &recoveryFakeUI{pickReturn: id}
	if err := clitasks.RunResumeWork(
		context.Background(), clitasks.ResumeWorkOptions{
			Stdin:  strings.NewReader(""),
			Stdout: io.Discard,
			Stderr: io.Discard,
			Agents: []codingagents.Agent{
				testutil.NewScriptedAgent(),
			},
			UI:      ui,
			JBinary: recoveryArgvJStub(t, argvPath),
		},
	); err != nil {
		t.Fatalf(
			"RunResumeWork from completed: %v; FSM edge "+
				"{completed, EventWorkResume, working} missing",
			err,
		)
	}
	got := recoveryReadStubArgv(t, argvPath)
	want := []string{
		"tasks", "orchestrate", "--id", id,
		"--phase=from-work", "--interactive=true",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}
