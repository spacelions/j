package orchestrator

import (
	"testing"

	"github.com/spacelions/j/internal/testutil"
)

// TestRowStoppedAtClarification_GetTaskError covers the GetTask error branch:
// an unknown task id makes GetTask return an error, which rowStoppedAtClarification
// treats as "not stopped" (returns false).
func TestRowStoppedAtClarification_GetTaskError(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	if rowStoppedAtClarification("ghost-id") {
		t.Fatal("expected false for unknown task id (GetTask error)")
	}
}
