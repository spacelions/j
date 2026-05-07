package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_VerifyBegin_NoLinearIssue_NoTraffic pins the
// "Task.LinearIssue == \"\" remains a full hook no-op" criterion for
// the new verify-begin PR-link branch: even with a non-empty
// PullRequestURL on EventVerifyBegin into Verifying, the hook must
// produce zero Linear HTTP traffic when the task is not linked to a
// Linear issue.
func TestLinearStateSync_VerifyBegin_NoLinearIssue_NoTraffic(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransitionWithPR("task-1", "",
		"https://github.com/spacelions/j/pull/42",
		tasks.StatusWorkDone, tasks.StatusVerifying,
		tasks.EventVerifyBegin)

	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected zero traffic, got %v", got)
	}
}
