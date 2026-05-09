package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_PendingApproval_AddsEyes pins the
// "👀 prefix on transition into plan-pending-approval"
// acceptance criterion. The eyes prefix is the only
// non-❗ decoration; verifying it lets us distinguish
// the approval-pending bucket from the abnormal bucket.
func TestLinearTitleSync_PendingApproval_AddsEyes(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	env.issueResp.Title = "Build pipeline"
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanning,
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
	if titleBody == "" {
		t.Fatalf("no title issueUpdate captured: %v", bodies)
	}
	if got := decodeMutationVar(t, titleBody, "title"); got !=
		"👀 Build pipeline" {
		t.Fatalf("title var = %q, want %q",
			got, "👀 Build pipeline")
	}
}
