package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_Verifying_MovesToInProgressNoComment pins
// the "after a task enters `verifying`, the linked Linear issue is
// moved to `In Progress` (no comment)" acceptance criterion. The
// verify stage shares the In Progress workflow state with the
// working stage; the only outbound mutation should be issueUpdate.
func TestLinearStateSync_Verifying_MovesToInProgressNoComment(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusWorkDone, tasks.StatusVerifying,
		tasks.EventVerifyBegin)

	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if v := decodeMutationVar(t, got[2], "stateId"); v != "s-prog" {
		t.Fatalf("issueUpdate stateId = %q, want s-prog", v)
	}
	for _, b := range got {
		if strings.Contains(b, "commentCreate") {
			t.Fatalf("unexpected commentCreate on verifying: %v",
				got)
		}
		if strings.Contains(b, "viewer{id") {
			t.Fatalf("unexpected viewer fetch on verifying: %v", got)
		}
	}
}
