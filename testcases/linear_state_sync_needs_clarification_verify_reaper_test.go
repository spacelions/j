package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_NeedsClarification_VerifyReaper covers the
// happy path for the verify reaper entry into needs-clarification.
func TestLinearStateSync_NeedsClarification_VerifyReaper(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")
	logPath := writeClarificationDir(t, "verifier asks Y")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransitionWithLog("task-1", "ENG-1", logPath,
		tasks.StatusVerifying, tasks.StatusNeedsClarification,
		tasks.EventReaperVerifyNeedsClarification)

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
		"verifier asks Y" {
		t.Fatalf("commentCreate body = %q, want %q",
			v, "verifier asks Y")
	}
	if v := decodeMutationVar(t, got[4], "id"); v != "node-1" {
		t.Fatalf("issueReminder id = %q, want node-1", v)
	}
}
