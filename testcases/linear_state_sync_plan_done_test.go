package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_PlanDone_MovesToTodoAndReminds pins the
// "after a task transitions to plan-done, the linked Linear issue
// is moved to Todo and a Linear inbox reminder is scheduled for
// the API-key owner" acceptance criterion. Order matters: the
// issue must be looked up before the team's workflow states are
// fetched, and the reminder must follow the state move so a UI
// watcher always sees the new state by the time the inbox ping
// lands.
func TestLinearStateSync_PlanDone_MovesToTodoAndReminds(
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
