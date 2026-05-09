package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_SwapEyesToAlert pins the reverse
// of the swap rule: a title carrying 👀 from
// plan-pending-approval MUST end up carrying ❗ — not
// "❗ 👀 …" — when the planner re-fails (verify-stuck)
// and the task moves to failed.
func TestLinearTitleSync_SwapEyesToAlert(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	env.issueResp.Title = "👀 Build pipeline"
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanPendingApproval,
		tasks.StatusFailed, tasks.EventVerifyStuck)

	bodies := env.recordedBodies()
	if len(bodies) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(bodies), bodies)
	}
	if got := decodeMutationVar(t, bodies[1], "title"); got !=
		"❗ Build pipeline" {
		t.Fatalf("title var = %q, want %q", got, "❗ Build pipeline")
	}
}
