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

// TestVerify_ResumePlan_SucceedsFromPlanning pins the planning
// self-loop on EventPlanResume: a row that crashed mid-planning with
// a recorded PlanResumeSession must be resumable without hitting the
// `cannot resume-plan task in status "planning"` guard. The
// orchestrate argv must include
// `--plan-requires-approval=true --interactive=true`.
func TestVerify_ResumePlan_SucceedsFromPlanning(t *testing.T) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusPlanning
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
			"RunResumePlan from planning: %v; FSM edge "+
				"{planning, EventPlanResume, planning} missing",
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
