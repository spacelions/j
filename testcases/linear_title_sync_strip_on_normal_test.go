package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_StripsAlertOnNormal pins the
// "no prefix when transitioning to a normal status —
// any prior ❗ or 👀 decoration must be removed"
// acceptance criterion. Resuming a previously-stuck task
// transitions failed → working, and the issue title that
// carried "❗ " must come back clean.
func TestLinearTitleSync_StripsAlertOnNormal(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	env.issueResp.Title = "❗ Build pipeline"
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusFailed, tasks.StatusWorking,
		tasks.EventWorkResume)

	bodies := env.recordedBodies()
	if len(bodies) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(bodies), bodies)
	}
	if got := decodeMutationVar(t, bodies[1], "title"); got !=
		"Build pipeline" {
		t.Fatalf("title var = %q, want %q", got, "Build pipeline")
	}
}
