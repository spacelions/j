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

// TestVerify_ReVerify_ClearsStaleVerifyResumeSession is the black-box
// pin for AC1 of "j tasks re-verify must start a brand-new
// verification, not try to resume a stale session": running
// `j tasks re-verify` against a `work-done` task that still carries a
// non-empty VerifyResumeSession (a leftover from a prior verify ->
// work-done cycle) must
//   - not surface the resume FSM error
//     ("cannot resume verify on task in status \"work-done\""),
//   - reach the orchestrator-spawn fake (verify-only re-exec), and
//   - leave VerifyResumeSession empty on the row so dispatchShellOut
//     in the spawned orchestrator child takes the fresh `Run` path
//     instead of the stale `RunResume`.
func TestVerify_ReVerify_ClearsStaleVerifyResumeSession(t *testing.T) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusWorkDone
		task.VerifyResumeSession = "stale-cursor"
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
		t.Fatalf(
			"RunReVerify with stale VerifyResumeSession: %v; "+
				"AC1 violated — re-verify must not bail out",
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
			"VerifyResumeSession = %q, want empty after re-verify; "+
				"a stale cursor would force dispatchShellOut down "+
				"the RunResume branch, which the FSM rejects from "+
				"work-done",
			row.VerifyResumeSession,
		)
	}
}
