package testcases_test

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestVerify_ReVerify_EmptyVerifyResumeSession_HappyPath pins AC3:
// for a `work-done` task that has no VerifyResumeSession, re-verify
// continues to behave exactly as before — it reaches the
// orchestrator-spawn fake (a `tasks orchestrate ... --phase=verify-only`
// re-exec) and the row's VerifyResumeSession remains empty (the
// new clear-on-stale block is a no-op when the cursor is already
// blank).
func TestVerify_ReVerify_EmptyVerifyResumeSession_HappyPath(
	t *testing.T,
) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusWorkDone
		task.VerifyResumeSession = ""
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
		t.Fatalf(
			"RunReVerify with empty VerifyResumeSession: %v; "+
				"AC3 violated — happy path must keep working",
			err,
		)
	}
	args := recoveryReadStubArgv(t, argvPath)
	if len(args) < 2 || args[0] != "tasks" ||
		args[1] != "orchestrate" {
		t.Fatalf(
			"argv = %v, want a `tasks orchestrate ...` re-exec",
			args,
		)
	}
	row := testutil.ReadTaskRow(t, id)
	if row.VerifyResumeSession != "" {
		t.Fatalf(
			"VerifyResumeSession = %q, want empty (no-op when "+
				"already blank)",
			row.VerifyResumeSession,
		)
	}
}
