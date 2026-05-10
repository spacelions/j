package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestPrune_FSM_ApplyRejectsIllegalTransition verifies acceptance
// criterion "Lifecycle status mutations still reject illegal
// transitions through the FSM transition table": Apply must return
// an error for transitions not in the table.
func TestPrune_FSM_ApplyRejectsIllegalTransition(t *testing.T) {
	cases := []struct {
		from  tasks.TaskStatus
		event tasks.Event
	}{
		// Plan-only events from non-planning statuses.
		{tasks.StatusWorking, tasks.EventPlanDone},
		{tasks.StatusWorking, tasks.EventPlanApprove},
		// Verify-only events from a sealed status.
		{tasks.StatusCompleted, tasks.EventVerifyPass},
		{tasks.StatusCompleted, tasks.EventVerifyFail},
		// Events that don't exist at all in the table.
		{tasks.StatusPlanning, tasks.EventWorkDone},
		{tasks.StatusWorking, tasks.EventVerifyPass},
	}
	for _, c := range cases {
		got, err := tasks.Apply(c.from, c.event)
		if err == nil {
			t.Errorf(
				"Apply(%q, %q) = %q, want error",
				c.from, c.event, got)
			continue
		}
		// Verify it's an IllegalTransitionError.
		ite, ok := err.(tasks.IllegalTransitionError)
		if !ok {
			t.Errorf(
				"Apply(%q, %q) error type = %T, "+
					"want IllegalTransitionError",
				c.from, c.event, err)
		}
		if ite.From != c.from {
			t.Errorf(
				"IllegalTransitionError.From = %q, want %q",
				ite.From, c.from)
		}
		if ite.Event != c.event {
			t.Errorf(
				"IllegalTransitionError.Event = %q, want %q",
				ite.Event, c.event)
		}
	}
}
