package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_Completed_MovesToInReview pins the
// completed-side acceptance criterion: a verify-pass transition
// moves the linked Linear issue to "In Review" and schedules a
// Linear inbox reminder for the API-key owner so they are paged to
// review the result.
func TestLinearStateSync_Completed_MovesToInReview(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusVerifying, tasks.StatusCompleted,
		tasks.EventVerifyPass)

	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "reminder",
	}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if v := decodeMutationVar(t, got[2], "stateId"); v != "s-rev" {
		t.Fatalf("issueUpdate stateId = %q, want s-rev", v)
	}
	if v := decodeMutationVar(t, got[3], "id"); v != "node-1" {
		t.Fatalf("issueReminder id = %q, want node-1", v)
	}
}
