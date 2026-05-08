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

// TestVerify_RePlan_SucceedsFromFailed pins acceptance criterion 1
// (failed half). The original FSM already covered this edge but the
// PR widens the test surface so we assert it survives the relaxed
// terminal-state guard.
func TestVerify_RePlan_SucceedsFromFailed(t *testing.T) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusFailed
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
		t.Fatalf("RunRePlan from failed: %v", err)
	}
	args := recoveryReadStubArgv(t, argvPath)
	if len(args) == 0 || args[0] != "tasks" {
		t.Fatalf("argv = %v, want spawned `tasks orchestrate`", args)
	}
}
