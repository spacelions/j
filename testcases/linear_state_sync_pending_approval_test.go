package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_PlanPendingApproval_MovesToTodo pins the
// "plan-pending-approval mirrors to Linear's Todo state" acceptance
// criterion. No comment is posted: the previous @-mention path was
// removed because Linear suppresses self-mentions.
func TestLinearStateSync_PlanPendingApproval_MovesToTodo(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanPendingApproval,
		tasks.EventPlanAwaitApproval)

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
		if strings.Contains(b, "commentCreate") {
			t.Fatalf("unexpected commentCreate: %v", got)
		}
	}
}
