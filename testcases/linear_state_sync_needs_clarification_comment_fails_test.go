package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_NeedsClarification_CommentFails_StillReminds
// pins rule #4 for a commentCreate transport failure: the call is
// attempted, fails, and a `commentCreate` warning is emitted, but
// the inbox reminder still fires independently.
func TestLinearStateSync_NeedsClarification_CommentFails_StillReminds(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	env.commentErrs = []string{"down"}
	saveLinearAPIKey(t, "lin_api_TEST")
	logPath := writeClarificationDir(t, "please clarify foo")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransitionWithLog("task-1", "ENG-1", logPath,
		tasks.StatusPlanning, tasks.StatusNeedsClarification,
		tasks.EventReaperPlanNeedsClarification)

	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate",
		"commentCreate", "reminder",
	}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if v := decodeMutationVar(t, got[4], "id"); v != "node-1" {
		t.Fatalf("issueReminder id = %q, want node-1", v)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "commentCreate") {
		t.Fatalf("stderr = %q, want commentCreate warning", msg)
	}
}
