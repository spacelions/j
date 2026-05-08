package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestFSM_ForegroundWorkNeedsClarification_LegalEdge pins the new
// foreground worker-exit edge: the event must be a legal edge from
// `working` and must land in `needs-clarification`, mirroring the
// existing planner-foreground assertion.
func TestFSM_ForegroundWorkNeedsClarification_LegalEdge(t *testing.T) {
	if !tasks.IsLegal(
		tasks.StatusWorking, tasks.EventWorkNeedsClarification) {
		t.Fatal("IsLegal(working, work_needs_clarification) = false," +
			" want true")
	}
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
