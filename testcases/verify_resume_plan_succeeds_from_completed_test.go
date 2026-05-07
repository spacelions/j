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

// TestVerify_ResumePlan_SucceedsFromCompleted pins acceptance
// criterion 4 (completed half): resume-plan must succeed for a
// completed row carrying a plan resume session. Backed by the new
// `{completed, EventPlanResume, planning}` FSM edge. The orchestrate
// argv must include `--plan-requires-approval=true --interactive=true`.
func TestVerify_ResumePlan_SucceedsFromCompleted(t *testing.T) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusCompleted
		task.PlanResumeSession = "sess-x"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &recoveryFakeUI{pickReturn: id}
	if err := clitasks.RunResumePlan(
		context.Background(), clitasks.ResumePlanOptions{
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
			"RunResumePlan from completed: %v; FSM edge "+
				"{completed, EventPlanResume, planning} missing",
			err,
		)
	}
	got := recoveryReadStubArgv(t, argvPath)
	want := []string{
		"tasks", "orchestrate", "--id", id,
		"--plan-requires-approval=true", "--interactive=true",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}
