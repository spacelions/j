package testcases_test

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestVerify_ResumePlan_SucceedsFromFailed pins acceptance criterion
// 4 (failed half): resume-plan must succeed for a failed row
// carrying a plan resume session. Backed by the new
// `{failed, EventPlanResume, planning}` FSM edge.
func TestVerify_ResumePlan_SucceedsFromFailed(t *testing.T) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusFailed
		task.PlanResumeSession = "sess-x"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &recoveryFakeUI{pickReturn: id}
	if err := clitasks.RunResumePlan(
		t.Context(), clitasks.ResumePlanOptions{
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
			"RunResumePlan from failed: %v; FSM edge "+
				"{failed, EventPlanResume, planning} missing",
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
