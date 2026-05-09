package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_NoLinearIssue_NoTraffic pins the
// "tasks not linked to a Linear issue MUST be unaffected
// (no API calls, no errors)" acceptance criterion. A task
// with empty LinearIssue MUST short-circuit before any
// HTTP traffic on the only transition where the title
// would otherwise be decorated.
func TestLinearTitleSync_NoLinearIssue_NoTraffic(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearTitleSync()
	fireStateSyncTransition("task-1", "",
		tasks.StatusPlanning, tasks.StatusFailed,
		tasks.EventVerifyStuck)

	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected zero traffic, got %v", got)
	}
}
