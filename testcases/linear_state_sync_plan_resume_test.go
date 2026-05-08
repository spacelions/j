package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_PlanResume_MovesToTodoNoReminder pins the
// "resume-plan from any source state moves the linked Linear issue
// to Todo with no inbox ping" acceptance criterion. Source state
// here is `failed`, exercising the cross-domain transition into
// StatusPlanning that the new state-sync table row enables.
func TestLinearStateSync_PlanResume_MovesToTodoNoReminder(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusFailed, tasks.StatusPlanning,
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
	for _, b := range got {
		if strings.Contains(b, "issueReminder") {
			t.Fatalf("unexpected issueReminder on plan-resume: %v",
				got)
		}
	}
}
