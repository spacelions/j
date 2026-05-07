package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestVerify_IsLegal_PlanResumeFromPendingApproval pins acceptance
// criterion 1: the FSM must legalize the EventPlanResume edge from
// StatusPlanPendingApproval. Without this edge, resume_plan.go's
// IsLegal guard rejects the transition with
// `J: cannot resume-plan task in status "plan-pending-approval"`.
func TestVerify_IsLegal_PlanResumeFromPendingApproval(t *testing.T) {
	if !tasks.IsLegal(
		tasks.StatusPlanPendingApproval,
		tasks.EventPlanResume,
	) {
		t.Fatal(
			"IsLegal(plan-pending-approval, plan_resume) = false, " +
				"want true; the new FSM edge is missing",
		)
	}
}
