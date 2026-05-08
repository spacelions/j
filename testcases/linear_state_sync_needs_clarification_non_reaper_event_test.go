package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_NeedsClarification_NonReaperEvent_NoExtras
// pins rule #5: a transition into needs-clarification driven by a
// non-reaper event must mirror the workflow state (R1) but must NOT
// post a clarification comment or schedule an inbox reminder.
func TestLinearStateSync_NeedsClarification_NonReaperEvent_NoExtras(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")
	logPath := writeClarificationDir(t, "please clarify foo")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransitionWithLog("task-1", "ENG-1", logPath,
		tasks.StatusPlanning, tasks.StatusNeedsClarification,
		tasks.EventPlanDone)

	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if v := decodeMutationVar(t, got[2], "stateId"); v != "s-prog" {
		t.Fatalf("issueUpdate stateId = %q, want s-prog", v)
	}
}
