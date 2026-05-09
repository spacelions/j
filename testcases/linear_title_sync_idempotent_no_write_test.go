package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_Idempotent_NoWrite pins the
// "operation MUST be idempotent: applying it twice with
// the same status produces the same title; if the title
// already has the correct prefix, no Linear API write is
// performed" acceptance criterion. The hook MUST issue
// the GetIssue lookup but MUST NOT issue a follow-up
// issueUpdate when the existing title is already
// decorated correctly for the destination status.
func TestLinearTitleSync_Idempotent_NoWrite(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	env.issueResp.Title = "❗ Build pipeline"
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusFailed,
		tasks.EventVerifyStuck)

	bodies := env.recordedBodies()
	if len(bodies) != 1 {
		t.Fatalf("want only the lookup, got %d: %v",
			len(bodies), bodies)
	}
	if !strings.Contains(bodies[0], "issue(id:") {
		t.Fatalf("expected issue lookup, got %s", bodies[0])
	}
	for _, b := range bodies {
		if strings.Contains(b, "issueUpdate") {
			t.Fatalf("unexpected issueUpdate write: %s", b)
		}
	}
}
