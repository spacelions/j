package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_RemindFails_WarnsButTransitionSucceeds pins
// the "a reminder failure stays a warn-and-return" acceptance
// criterion. When issueReminder surfaces a GraphQL error the hook
// must emit a `linear sync: issueReminder: ...` warning to stderr
// and tasks.Notify must still return normally so the FSM advance
// is not blocked.
func TestLinearStateSync_RemindFails_WarnsButTransitionSucceeds(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	env.remindErrors = []string{"down"}
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)

	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "reminder",
	}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	msg := env.stderrText(t)
	if !strings.Contains(msg, "issueReminder") {
		t.Fatalf("stderr = %q, want issueReminder warning", msg)
	}
	if !strings.Contains(msg, "linear sync") {
		t.Fatalf("stderr = %q, want linear sync prefix", msg)
	}
}
