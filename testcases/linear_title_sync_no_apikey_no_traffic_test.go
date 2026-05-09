package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_NoAPIKey_NoTraffic pins the
// "when no Linear API key is configured, the feature
// MUST be a silent no-op, matching sibling Linear
// hooks" acceptance criterion. The black-box
// invariant is that no HTTP traffic leaves the host
// when the key is absent — sibling hooks emit a
// stderr advisory under the same circumstances, but
// they all stop short of the network.
func TestLinearTitleSync_NoAPIKey_NoTraffic(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusFailed,
		tasks.EventVerifyStuck)

	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected zero traffic, got %v", got)
	}
}
