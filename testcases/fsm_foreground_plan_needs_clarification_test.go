package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestFSM_ForegroundPlanNeedsClarification_LegalEdge pins acceptance
// criterion 3 from plan.md: the foreground planner-exit event must be
// a legal edge from `planning` and must land in `needs-clarification`.
func TestFSM_ForegroundPlanNeedsClarification_LegalEdge(t *testing.T) {
	if !tasks.IsLegal(
		tasks.StatusPlanning, tasks.EventPlanNeedsClarification) {
		t.Fatal("IsLegal(planning, plan_needs_clarification) = false," +
			" want true")
	}
	got, err := tasks.Apply(
		tasks.StatusPlanning, tasks.EventPlanNeedsClarification)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got != tasks.StatusNeedsClarification {
		t.Fatalf("Apply = %q, want %q",
			got, tasks.StatusNeedsClarification)
	}
}
