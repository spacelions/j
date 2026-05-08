package testcases_test

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestVerifierSPA48_RemindSuccess_WritesNothing pins acceptance
// criterion F: when RemindOnIssue returns nil, the helper writes
// nothing to agent.log and nothing to stderr. The FSM call
// sequence is unaffected (issue → states → issueUpdate → reminder).
func TestVerifierSPA48_RemindSuccess_WritesNothing(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")
	logPath := agentLogPathOnlyDir(t)

	lifecycle.InitLinearStateSync()
	fireStateSyncTransitionWithLog("task-1", "ENG-1", logPath,
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)

	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "reminder",
	}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if _, err := os.Stat(logPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("agent.log Stat err = %v, want not-exist", err)
	}
	if msg := env.stderrText(t); strings.Contains(
		msg, "issueReminder") {
		t.Fatalf("stderr = %q, want clean stderr", msg)
	}
}
