package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_PlanPendingApproval_MovesToTodoAndMentions
// pins the second half of the "plan stage -> Todo + mention"
// acceptance criterion: `plan-pending-approval` must mirror to
// `Todo` exactly like `plan-done`, and the same `@<owner> todo`
// mention comment must follow so the user is paged whether the
// plan auto-approves or queues for review.
func TestLinearStateSync_PlanPendingApproval_MovesToTodoAndMentions(
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
		"issue", "states", "issueUpdate", "viewer", "commentCreate",
	}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if v := decodeMutationVar(t, got[2], "stateId"); v != "s-todo" {
		t.Fatalf("issueUpdate stateId = %q, want s-todo", v)
	}
	if v := decodeMutationVar(t, got[4], "body"); v != "@user-uuid todo" {
		t.Fatalf("commentCreate body = %q, want '@user-uuid todo'", v)
	}
}
