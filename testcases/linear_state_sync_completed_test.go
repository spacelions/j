package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_Completed_MovesToInReview pins the
// completed-side acceptance criterion: a verify-pass transition
// moves the linked Linear issue to "In Review" with no follow-up
// commentCreate — the @-mention path was removed because Linear
// suppresses self-mentions and the comment was therefore silent
// noise.
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
	want := []string{"issue", "states", "issueUpdate"}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if v := decodeMutationVar(t, got[2], "stateId"); v != "s-rev" {
		t.Fatalf("issueUpdate stateId = %q, want s-rev", v)
	}
	for _, b := range got {
		if strings.Contains(b, "commentCreate") {
			t.Fatalf("unexpected commentCreate on completed: %v", got)
		}
	}
}
