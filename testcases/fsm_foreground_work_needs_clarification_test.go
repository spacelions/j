package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestFSM_ForegroundWorkNeedsClarification_LegalEdge pins the new
// foreground worker-exit edge: the event must move from `working`
// to `needs-clarification`, mirroring the existing
// planner-foreground assertion.
func TestFSM_ForegroundWorkNeedsClarification_LegalEdge(t *testing.T) {
	got, err := tasks.Apply(
		tasks.StatusWorking, tasks.EventWorkNeedsClarification)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got != tasks.StatusNeedsClarification {
		t.Fatalf("Apply = %q, want %q",
			got, tasks.StatusNeedsClarification)
	}
}
