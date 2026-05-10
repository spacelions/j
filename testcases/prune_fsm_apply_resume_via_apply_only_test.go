package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestPrune_FSM_ApplyResumeViaApplyOnly verifies acceptance
// criterion "Resume flows do not use a standalone FSM legality
// check before attempting to resume a task": resume transitions must
// succeed through Apply (the only mutation path) without a separate
// IsLegal preflight.
func TestPrune_FSM_ApplyResumeViaApplyOnly(t *testing.T) {
	cases := []struct {
		from  tasks.TaskStatus
		event tasks.Event
		want  tasks.TaskStatus
	}{
		{tasks.StatusCompleted, tasks.EventPlanResume, tasks.StatusPlanning},
		{tasks.StatusCompleted, tasks.EventWorkResume, tasks.StatusWorking},
		{tasks.StatusCompleted, tasks.EventVerifyResume, tasks.StatusVerifying},
		{tasks.StatusFailed, tasks.EventPlanResume, tasks.StatusPlanning},
		{tasks.StatusFailed, tasks.EventWorkResume, tasks.StatusWorking},
		{tasks.StatusFailed, tasks.EventVerifyResume, tasks.StatusVerifying},
		{tasks.StatusHelp, tasks.EventPlanResume, tasks.StatusPlanning},
		{tasks.StatusHelp, tasks.EventWorkResume, tasks.StatusWorking},
		{tasks.StatusHelp, tasks.EventVerifyResume, tasks.StatusVerifying},
		{tasks.StatusNeedsClarification, tasks.EventPlanResume, tasks.StatusPlanning},
		{tasks.StatusNeedsClarification, tasks.EventWorkResume, tasks.StatusWorking},
		{tasks.StatusNeedsClarification, tasks.EventVerifyResume, tasks.StatusVerifying},
		{
			tasks.StatusPlanPendingApproval, tasks.EventPlanResume,
			tasks.StatusPlanning,
		},
		{tasks.StatusPlanDone, tasks.EventPlanResume, tasks.StatusPlanning},
		{tasks.StatusPlanning, tasks.EventPlanResume, tasks.StatusPlanning},
		{tasks.StatusWorking, tasks.EventWorkResume, tasks.StatusWorking},
		{tasks.StatusVerifying, tasks.EventVerifyResume, tasks.StatusVerifying},
	}
	for _, c := range cases {
		got, err := tasks.Apply(c.from, c.event)
		if err != nil {
			t.Errorf(
				"Apply(%q, %q): %v",
				c.from, c.event, err,
			)
			continue
		}
		if got != c.want {
			t.Errorf(
				"Apply(%q, %q) = %q, want %q",
				c.from, c.event, got, c.want,
			)
		}
	}
}
