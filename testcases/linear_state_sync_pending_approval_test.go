package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_PlanPendingApproval_MovesToTodoAndReminds
// pins the second half of the "plan stage -> Todo + reminder"
// acceptance criterion: `plan-pending-approval` must mirror to
// `Todo` exactly like `plan-done`, and a Linear inbox reminder
// must follow so the user is paged whether the plan auto-approves
// or queues for review.
func TestLinearStateSync_PlanPendingApproval_MovesToTodoAndReminds(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanPendingApproval,
		tasks.EventPlanAwaitApproval)

	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "remindMe",
	}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if v := decodeMutationVar(t, got[2], "stateId"); v != "s-todo" {
		t.Fatalf("issueUpdate stateId = %q, want s-todo", v)
	}
	if v := decodeMutationVar(t, got[3], "id"); v != "node-1" {
		t.Fatalf("issueRemindMe id = %q, want node-1", v)
	}
}
