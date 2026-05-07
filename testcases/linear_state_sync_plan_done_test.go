package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_PlanDone_MovesToTodoAndMentions pins the
// "after a task transitions to plan-done, the linked Linear issue
// is moved to Todo and a `@<owner> todo` mention comment appears"
// acceptance criterion. Order matters: the issue must be looked up
// before the team's workflow states are fetched, and the mention
// comment must follow the state move so a UI watcher always sees
// the new state by the time the comment lands.
func TestLinearStateSync_PlanDone_MovesToTodoAndMentions(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)

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
