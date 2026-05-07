package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestVerify_Apply_PlanResumeFromPendingApproval pins acceptance
// criterion 1's destination half: applying EventPlanResume from
// StatusPlanPendingApproval must transition the task back to
// StatusPlanning so the planner can pick up where it left off.
func TestVerify_Apply_PlanResumeFromPendingApproval(t *testing.T) {
	got, err := tasks.Apply(
		tasks.StatusPlanPendingApproval,
		tasks.EventPlanResume,
	)
	if err != nil {
		t.Fatalf(
			"Apply(plan-pending-approval, plan_resume): %v, "+
				"want no error",
			err,
		)
	}
	if got != tasks.StatusPlanning {
		t.Fatalf(
			"Apply(plan-pending-approval, plan_resume) = %q, "+
				"want %q",
			got,
			tasks.StatusPlanning,
		)
	}
}
