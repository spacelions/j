package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestFSM_WorkResumeFromNeedsClarification pins acceptance
// criterion 5: once the user resolves the worker clarification and
// runs `j tasks continue`, the dispatch fires `EventWorkResume`.
// That edge must transition from `needs-clarification` back to
// `working` so the orchestrator can re-run the worker and (on a
// clean exit) progress to the verifier.
func TestFSM_WorkResumeFromNeedsClarification(t *testing.T) {
	got, err := tasks.Apply(
		tasks.StatusNeedsClarification, tasks.EventWorkResume)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got != tasks.StatusWorking {
		t.Fatalf("Apply = %q, want %q", got, tasks.StatusWorking)
	}
}
