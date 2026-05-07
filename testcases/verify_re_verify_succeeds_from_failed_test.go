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
	false_ := false
	if err := clitasks.RunReVerify(
		context.Background(), clitasks.ReVerifyOptions{
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
		t.Fatalf("RunReVerify from failed: %v", err)
	}
	args := recoveryReadStubArgv(t, argvPath)
	if len(args) == 0 || args[0] != "tasks" {
		t.Fatalf("argv = %v, want spawned `tasks orchestrate`", args)
	}
}
