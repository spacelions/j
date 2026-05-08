package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestVerifierSPA48_RemindFails_NoLogPath_Silent pins acceptance
// criterion B: when RemindOnIssue returns a non-nil error and
// task.AgentLogPath is empty, the failure must be dropped silently
// — no stderr write and no fall-back. The mutation is still
// attempted, so the call sequence keeps the trailing `reminder`.
func TestVerifierSPA48_RemindFails_NoLogPath_Silent(t *testing.T) {
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
		t.Fatalf(
			"stderr = %q, want no linear sync issueReminder", msg)
	}
}
