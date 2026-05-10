package testcases_test

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestVerify_ResumeVerify_SucceedsFromVerifyingStatus is the primary
// black-box pin for SPA-86: the original bug left users stuck because
// the lock file's stale phase pointed them at `j tasks resume-work`,
// which the FSM rejected with "cannot resume-work task in status
// 'verifying'". The symmetric issue existed for resume-verify: the FSM
// blocked EventVerifyResume from StatusVerifying, so the user had no
// valid command.
//
// After the fix the FSM legality gate is removed from resume-verify;
// only the artifact gate matters. A task in StatusVerifying with
// plan.md present and WorkBeginAt set must succeed.
func TestVerify_ResumeVerify_SucceedsFromVerifyingStatus(t *testing.T) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusVerifying
		task.WorkBeginAt = time.Now().UTC().Add(-time.Hour)
		task.VerifyResumeSession = "sess-y"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &recoveryFakeUI{pickReturn: id}
	if err := clitasks.RunResumeVerify(
		t.Context(), clitasks.ResumeVerifyOptions{
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
			"RunResumeVerify from verifying: %v; "+
				"FSM legality gate should not block resume-verify "+
				"from StatusVerifying after SPA-86 fix",
			err,
		)
	}
	got := recoveryReadStubArgv(t, argvPath)
	want := []string{
		"tasks", "orchestrate", "--id", id,
		"--phase=verify-only", "--interactive=true",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}
