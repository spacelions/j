package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_PlanRestart_MovesToTodoNoReminder pins the
// "re-plan from any source state moves the linked Linear issue back
// to Todo, with no inbox reminder" acceptance criterion. Re-plan is
// user-initiated, so the only outbound mutation must be issueUpdate
// pointing at the s-todo workflow state — no issueRemindMe.
func TestLinearStateSync_PlanRestart_MovesToTodoNoReminder(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusWorking, tasks.StatusPlanning,
		tasks.EventPlanRestart)

	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if v := decodeMutationVar(t, got[2], "stateId"); v != "s-todo" {
		t.Fatalf("issueUpdate stateId = %q, want s-todo", v)
	}
	for _, b := range got {
		if strings.Contains(b, "issueRemindMe") {
			t.Fatalf("unexpected issueRemindMe on plan-restart: %v",
				got)
		}
	}
}
