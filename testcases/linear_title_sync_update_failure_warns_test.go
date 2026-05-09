package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_UpdateFailureWarns pins the
// "title update MUST NOT block the FSM transition.
// Failures (network, auth, GraphQL error) MUST be
// logged in the same best-effort style as the existing
// Linear sync hooks and MUST NOT propagate"
// acceptance criterion. A GraphQL-level rejection of
// the issueUpdate must reach stderr (the agent-log
// channel siblings already use) and must not panic.
func TestLinearTitleSync_UpdateFailureWarns(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	env.issueResp.Title = "Build pipeline"
	env.updateErrors = []string{"boom"}
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusFailed,
		tasks.EventVerifyStuck)

	bodies := env.recordedBodies()
	if len(bodies) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(bodies), bodies)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "issueUpdate title") {
		t.Fatalf("stderr = %q, want title-update warning", msg)
	}
}
