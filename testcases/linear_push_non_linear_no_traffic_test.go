package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearPush_NonLinearTask_NoTraffic pins the
// "non-Linear-sourced task triggers zero Linear HTTP traffic"
// acceptance criterion: when LinearIssue == "" the hook short-
// circuits before any HTTP call leaves the host.
func TestLinearPush_NonLinearTask_NoTraffic(t *testing.T) {
	id := tasks.NewTaskID()
	env := newLinearPushEnv(t, id, "req", "plan")
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearPush()
	firePlanDone(id, "", tasks.EventPlanDone)

	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected zero linear HTTP traffic, got %d: %v",
			len(got), got)
	}
}
