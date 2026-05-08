package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_NeedsClarification_PlanReaper covers the
// happy path of SPA-44 for the plan reaper:
//   - workflow state mirrored to "In Progress" (R1)
//   - clarification.md body posted as a Linear comment (R2)
//   - inbox reminder scheduled on the issue (R3)
func TestLinearStateSync_NeedsClarification_PlanReaper(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
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
	if v := decodeMutationVar(t, got[2], "stateId"); v != "s-prog" {
		t.Fatalf("issueUpdate stateId = %q, want s-prog", v)
	}
	if v := decodeMutationVar(t, got[3], "body"); v !=
		"please clarify foo" {
		t.Fatalf("commentCreate body = %q, want clarification body",
			v)
	}
	if v := decodeMutationVar(t, got[4], "id"); v != "node-1" {
		t.Fatalf("issueReminder id = %q, want node-1", v)
	}
}
