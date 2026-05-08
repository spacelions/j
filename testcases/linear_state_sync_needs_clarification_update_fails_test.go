package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_NeedsClarification_UpdateFails_StillPosts
// pins rule #4 for a failing issueUpdate: the workflow-state mutation
// fails with a warning, but the comment + reminder follow-ups must
// still both run.
func TestLinearStateSync_NeedsClarification_UpdateFails_StillPosts(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	env.updateErrors = []string{"boom"}
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
	if v := decodeMutationVar(t, got[3], "body"); v !=
		"please clarify foo" {
		t.Fatalf("commentCreate body = %q, want clarification body",
			v)
	}
	if v := decodeMutationVar(t, got[4], "id"); v != "node-1" {
		t.Fatalf("issueReminder id = %q, want node-1", v)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "issueUpdate") {
		t.Fatalf("stderr = %q, want issueUpdate warning", msg)
	}
}
