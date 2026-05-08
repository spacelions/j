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

// TestVerify_ReVerify_SucceedsFromFailed pins acceptance criterion 3
// (failed half). The failed→verifying restart edge already existed
// pre-PR; this guards against a regression in the relaxed terminal
// guard.
func TestVerify_ReVerify_SucceedsFromFailed(t *testing.T) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusFailed
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &recoveryFakeUI{statusOK: true}

	if err := clitasks.RunReVerify(
		t.Context(), clitasks.ReVerifyOptions{
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
		t.Fatalf("RunReVerify from failed: %v", err)
	}
	args := recoveryReadStubArgv(t, argvPath)
	if len(args) == 0 || args[0] != "tasks" {
		t.Fatalf("argv = %v, want spawned `tasks orchestrate`", args)
	}
}
