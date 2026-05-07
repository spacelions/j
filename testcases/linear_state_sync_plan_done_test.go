package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_PlanDone_MovesToTodo pins the "after a task
// transitions to plan-done, the linked Linear issue is moved to
// Todo" acceptance criterion. Order matters: the issue must be
// looked up before the team's workflow states are fetched. No
// comment is posted: the previous @-mention path was removed
// because Linear suppresses self-mentions.
func TestLinearStateSync_PlanDone_MovesToTodo(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
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
	if v := decodeMutationVar(t, got[2], "stateId"); v != "s-todo" {
		t.Fatalf("issueUpdate stateId = %q, want s-todo", v)
	}
	for _, b := range got {
		if strings.Contains(b, "commentCreate") {
			t.Fatalf("unexpected commentCreate: %v", got)
		}
	}
}
