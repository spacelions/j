package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_WorkRestart_StaysInProgressNoReminder pins
// the "re-work continues to set In Progress (no reminder)"
// acceptance criterion. The existing s-prog mapping for
// StatusWorking must still fire when the transition is driven by
// EventWorkRestart, and no inbox ping should be scheduled.
func TestLinearStateSync_WorkRestart_StaysInProgressNoReminder(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusWorkDone, tasks.StatusWorking,
		tasks.EventWorkRestart)

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
		if strings.Contains(b, "issueReminder") {
			t.Fatalf("unexpected issueReminder on re-work: %v", got)
		}
	}
}
