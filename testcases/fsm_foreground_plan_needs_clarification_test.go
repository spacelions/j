package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestFSM_ForegroundPlanNeedsClarification_LegalEdge pins acceptance
// criterion 3 from plan.md: the foreground planner-exit event must be
// able to move from `planning` to `needs-clarification`.
func TestFSM_ForegroundPlanNeedsClarification_LegalEdge(t *testing.T) {
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
