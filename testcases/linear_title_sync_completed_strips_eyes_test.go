package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_CompletedStripsEyes pins the
// "transition to any other status — i.e., back to a
// normal status such as planning, plan-done, working,
// work-done, verifying, or completed — MUST remove any
// prior decoration" rule, exercised on the terminal
// completed status which is reached from verifying via
// EventVerifyPass. A title decorated with 👀 from a
// historical pending-approval state must come back
// clean even though the transition is plan→completed
// rather than abnormal→working.
func TestLinearTitleSync_CompletedStripsEyes(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	env.issueResp.Title = "👀 Build pipeline"
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusVerifying, tasks.StatusCompleted,
		tasks.EventVerifyPass)

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
		"Build pipeline" {
		t.Fatalf("title var = %q, want %q", got, "Build pipeline")
	}
}
