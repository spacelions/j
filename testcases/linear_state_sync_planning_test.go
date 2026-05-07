package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_PlanResume_MovesToTodo pins the
// "EventPlanResume re-enters planning and mirrors to Linear's Todo"
// acceptance criterion. Without the StatusPlanning entry in the
// hook's state-sync table, resuming a paused / approved-but-
// re-planned task would leave Linear stuck in In Progress / In
// Review.
func TestLinearStateSync_PlanResume_MovesToTodo(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanPendingApproval, tasks.StatusPlanning,
		tasks.EventPlanResume)

	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if v := decodeMutationVar(t, got[2], "stateId"); v != "s-todo" {
		t.Fatalf("issueUpdate stateId = %q, want s-todo", v)
	}
}

// TestLinearStateSync_PlanRestart_MovesToTodo pins the
// "EventPlanRestart from a completed task moves Linear back to
// Todo" acceptance criterion: a task being re-planned after the
// fact must drag the upstream issue back to the Todo column so the
// progression remains monotonic.
func TestLinearStateSync_PlanRestart_MovesToTodo(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusCompleted, tasks.StatusPlanning,
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
}
