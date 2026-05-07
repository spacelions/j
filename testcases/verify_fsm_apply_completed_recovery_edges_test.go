package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestVerify_FSM_Apply_CompletedRecoveryEdges pins the six new
// outgoing edges from `completed`: each restart/resume event must
// land in the matching phase status. Without these edges every
// recovery command reverts to its old `cannot <cmd> task in status
// "completed"` short-circuit.
func TestVerify_FSM_Apply_CompletedRecoveryEdges(t *testing.T) {
	cases := []struct {
		ev   tasks.Event
		want tasks.TaskStatus
	}{
		{tasks.EventPlanRestart, tasks.StatusPlanning},
		{tasks.EventPlanResume, tasks.StatusPlanning},
		{tasks.EventWorkRestart, tasks.StatusWorking},
		{tasks.EventWorkResume, tasks.StatusWorking},
		{tasks.EventVerifyRestart, tasks.StatusVerifying},
		{tasks.EventVerifyResume, tasks.StatusVerifying},
	}
	for _, c := range cases {
		got, err := tasks.Apply(tasks.StatusCompleted, c.ev)
		if err != nil {
			t.Errorf(
				"Apply(completed, %q) error: %v", c.ev, err)
			continue
		}
		if got != c.want {
			t.Errorf(
				"Apply(completed, %q) = %q, want %q",
				c.ev, got, c.want)
		}
	}
}
