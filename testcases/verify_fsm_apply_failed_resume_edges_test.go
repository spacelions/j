package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestVerify_FSM_Apply_FailedResumeEdges pins the three new resume
// edges out of `failed`: plan_resume, work_resume, verify_resume.
// The pre-PR FSM only carried the *_restart variants out of failed,
// rejecting `resume-*` from a crashed task.
func TestVerify_FSM_Apply_FailedResumeEdges(t *testing.T) {
	cases := []struct {
		ev   tasks.Event
		want tasks.TaskStatus
	}{
		{tasks.EventPlanResume, tasks.StatusPlanning},
		{tasks.EventWorkResume, tasks.StatusWorking},
		{tasks.EventVerifyResume, tasks.StatusVerifying},
	}
	for _, c := range cases {
		got, err := tasks.Apply(tasks.StatusFailed, c.ev)
		if err != nil {
			t.Errorf(
				"Apply(failed, %q) error: %v", c.ev, err)
			continue
		}
		if got != c.want {
			t.Errorf(
				"Apply(failed, %q) = %q, want %q",
				c.ev, got, c.want)
		}
	}
}
