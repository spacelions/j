package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearPush_NeedsClarificationDestination_NoHTTP pins
// acceptance criterion 5 from plan.md: linearPushHook must early-return
// when `tr.To` is `needs-clarification` even if `tr.Event` matches
// `isPlanSuccessEvent`. Defence-in-depth — protects against any
// future event landing outside `plan-done` /
// `plan-pending-approval` from triggering a missing-`plan.md` upload.
func TestLinearPush_NeedsClarificationDestination_NoHTTP(t *testing.T) {
	id := tasks.NewTaskID()
	env := newLinearPushEnv(t, id, "REQ", "PLAN")
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearPush()

	tasks.Notify(
		tasks.Transition{
			From:  tasks.StatusPlanning,
			Event: tasks.EventPlanDone,
			To:    tasks.StatusNeedsClarification,
		},
		tasks.Task{
			ID:          id,
			Status:      tasks.StatusNeedsClarification,
			LinearIssue: "ENG-1",
		},
	)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %d: %v",
			len(got), got)
	}
}
