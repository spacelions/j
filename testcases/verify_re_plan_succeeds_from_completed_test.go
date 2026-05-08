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

// TestVerify_RePlan_SucceedsFromCompleted pins acceptance criterion 1
// (completed half): `j tasks re-plan --from-task <id>` must succeed
// once the user accepts the status-override prompt. Without the new
// `{completed, EventPlanRestart, planning}` FSM edge the command
// errors with `cannot re-plan task in status "completed"`.
func TestVerify_RePlan_SucceedsFromCompleted(t *testing.T) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusCompleted
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &recoveryFakeUI{statusOK: true}

	if err := clitasks.RunRePlan(
		t.Context(), clitasks.RePlanOptions{
			FromTask:    id,
			Interactive: new(false),
			Stdin:       strings.NewReader(""),
			Stdout:      io.Discard,
			Stderr:      io.Discard,
			Agents: []codingagents.Agent{
				testutil.NewScriptedAgent(),
			},
			UI:      ui,
			JBinary: recoveryArgvJStub(t, argvPath),
		},
	); err != nil {
		t.Fatalf(
			"RunRePlan from completed: %v; FSM edge "+
				"{completed, EventPlanRestart, planning} missing",
			err,
		)
	}
	args := recoveryReadStubArgv(t, argvPath)
	if len(args) == 0 || args[0] != "tasks" {
		t.Fatalf("argv = %v, want spawned `tasks orchestrate`", args)
	}
}
