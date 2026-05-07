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

// TestVerify_ReWork_SucceedsFromCompleted pins acceptance criterion 2
// (completed half): re-work must clear the IsLegal guard for a
// completed task once the override prompt is accepted. Backed by the
// new `{completed, EventWorkRestart, working}` FSM edge.
func TestVerify_ReWork_SucceedsFromCompleted(t *testing.T) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusCompleted
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &recoveryFakeUI{statusOK: true}
	false_ := false
	if err := clitasks.RunReWork(
		context.Background(), clitasks.ReWorkOptions{
			FromTask:    id,
			Interactive: &false_,
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
			"RunReWork from completed: %v; FSM edge "+
				"{completed, EventWorkRestart, working} missing",
			err,
		)
	}
	args := recoveryReadStubArgv(t, argvPath)
	if len(args) == 0 || args[0] != "tasks" {
		t.Fatalf("argv = %v, want spawned `tasks orchestrate`", args)
	}
}
