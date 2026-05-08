package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_RemindFails_NoLogPath_SilentDrop pins the
// SPA-48 silent-drop branch: a foreground / interactive transition
// has no per-task agent.log, so when issueReminder fails the hook
// must drop the error silently — no terminal noise, no filesystem
// write. The call sequence (issue → states → issueUpdate →
// reminder) is preserved because the request is still attempted;
// only the failure log is suppressed.
func TestLinearStateSync_RemindFails_NoLogPath_SilentDrop(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	env.remindErrors = []string{"down"}
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransitionWithLog("task-1", "ENG-1", "",
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
	if strings.Contains(msg, "issueReminder") {
		t.Fatalf("stderr = %q, want no issueReminder leak", msg)
	}
	if strings.Contains(msg, "linear sync: issueReminder") {
		t.Fatalf("stderr = %q, want no linear sync leak", msg)
	}
}
