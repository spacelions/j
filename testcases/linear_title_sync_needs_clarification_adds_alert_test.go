package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_NeedsClarification_AddsAlert pins
// the "❗ prefix on transition into needs-clarification"
// acceptance criterion. needs-clarification is the
// third member of the ❗ family alongside failed and
// help, and the lifecycle state most worth flagging in
// the inbox: the worker has stopped and is waiting on
// the human.
func TestLinearTitleSync_NeedsClarification_AddsAlert(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	env.issueResp.Title = "Refactor pipeline"
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanning,
		tasks.StatusNeedsClarification,
		tasks.EventPlanNeedsClarification)

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
		"❗ Refactor pipeline" {
		t.Fatalf("title var = %q, want %q",
			got, "❗ Refactor pipeline")
	}
}
