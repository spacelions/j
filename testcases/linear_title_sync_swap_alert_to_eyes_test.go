package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_SwapAlertToEyes pins the
// "transitions between an abnormal status and the
// approval-pending status MUST swap the prefix correctly
// (❗ ↔ 👀, not stack them)" rule. A title carrying ❗
// from a previous needs-clarification must end up
// carrying exactly one 👀 — not "👀 ❗ …" — when the
// task moves to plan-pending-approval.
func TestLinearTitleSync_SwapAlertToEyes(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	env.issueResp.Title = "❗ Build pipeline"
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusNeedsClarification,
		tasks.StatusPlanPendingApproval,
		tasks.EventPlanAwaitApproval)

	bodies := env.recordedBodies()
	if len(bodies) < 2 {
		t.Fatalf("want >=2 calls, got %d: %v", len(bodies), bodies)
	}
	var titleBody string
	for _, b := range bodies {
		if decodeMutationVar(t, b, "title") != "" {
			titleBody = b
			break
		}
	}
	if got := decodeMutationVar(t, titleBody, "title"); got !=
		"👀 Build pipeline" {
		t.Fatalf("title var = %q, want %q",
			got, "👀 Build pipeline")
	}
}
