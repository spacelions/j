package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_NeedsClarification_RemindFails_LogsToAgentLog
// pins SPA-48 against the needs-clarification branch: when the
// trailing issueReminder mutation fails, the marker is appended to
// the per-task agent.log instead of painting an orange dialog onto
// stderr. The call sequence (issue → states → issueUpdate →
// commentCreate → reminder) is unaffected.
func TestLinearStateSync_NeedsClarification_RemindFails_LogsToAgentLog(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	env.remindErrors = []string{"down"}
	saveLinearAPIKey(t, "lin_api_TEST")
	logPath := writeClarificationDir(t, "please clarify foo")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransitionWithLog("task-1", "ENG-1", logPath,
		tasks.StatusPlanning, tasks.StatusNeedsClarification,
		tasks.EventReaperPlanNeedsClarification)

	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate",
		"commentCreate", "reminder",
	}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	logged := readAgentLog(t, logPath)
	if !strings.Contains(logged, "linear reminder_failed") {
		t.Fatalf("agent.log = %q, want reminder_failed marker",
			logged)
	}
	if !strings.Contains(logged, "issue=node-1") {
		t.Fatalf("agent.log = %q, want issue=node-1", logged)
	}
	if msg := env.stderrText(t); strings.Contains(
		msg, "issueReminder") {
		t.Fatalf("stderr = %q, want no issueReminder leak", msg)
	}
}
