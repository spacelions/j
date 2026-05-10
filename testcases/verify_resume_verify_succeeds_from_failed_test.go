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

// TestVerify_ResumeVerify_SucceedsFromFailed pins acceptance criterion
// 6 (failed half): resume-verify must succeed for a failed row
// carrying a verify resume session. Backed by the new
// `{failed, EventVerifyResume, verifying}` FSM edge.
func TestVerify_ResumeVerify_SucceedsFromFailed(t *testing.T) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusFailed
		task.VerifyResumeSession = "sess-x"
		task.WorkBeginAt = time.Now().UTC()
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
			"RunResumeVerify from failed: %v; FSM edge "+
				"{failed, EventVerifyResume, verifying} missing",
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
