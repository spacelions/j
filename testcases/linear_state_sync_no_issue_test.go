package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_NoLinearIssue_NoTraffic pins the "tasks
// without a LinearIssue produce zero Linear HTTP traffic on
// transitions" acceptance criterion. A markdown-sourced task firing
// the same plan-done transition must not contact Linear under any
// circumstance — the LinearIssue==""  guard short-circuits the hook
// before any of GetIssue / ListStates / issueUpdate is reached.
func TestLinearStateSync_NoLinearIssue_NoTraffic(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)

	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected zero traffic, got %v", got)
	}
}
