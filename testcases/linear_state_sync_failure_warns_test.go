package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_UpdateFails_Warns pins the best-effort
// acceptance criterion: a Linear API error at the issueUpdate step
// warns to stderr (same `linear sync:` box) and does NOT block
// the transition. tasks.Notify returning normally proves the FSM
// is unaffected; the stderr warning proves the user is informed.
func TestLinearStateSync_UpdateFails_Warns(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	env.updateErrors = []string{"boom"}
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)

	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "linear sync") {
		t.Fatalf("stderr = %q, want a 'linear sync' warning", msg)
	}
}
