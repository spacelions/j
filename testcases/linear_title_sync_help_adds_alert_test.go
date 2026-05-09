package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_Help_AddsAlert pins the
// "❗ prefix on transition into help" acceptance
// criterion. The help status is one of the three
// abnormal/needs-attention statuses that share the ❗
// decoration; verifying it independently from failed and
// needs-clarification guards against an off-by-one in
// prefixFor's switch arm.
func TestLinearTitleSync_Help_AddsAlert(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	env.issueResp.Title = "Build pipeline"
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusWorking, tasks.StatusHelp,
		tasks.EventWorkError)

	bodies := env.recordedBodies()
	if len(bodies) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(bodies), bodies)
	}
	if got := decodeMutationVar(t, bodies[1], "title"); got !=
		"❗ Build pipeline" {
		t.Fatalf("title var = %q, want %q", got, "❗ Build pipeline")
	}
}
