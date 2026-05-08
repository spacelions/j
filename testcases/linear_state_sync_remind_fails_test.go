package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_RemindFails_LogsToAgentLogButTransitionSucceeds
// pins SPA-48: when issueReminder surfaces a GraphQL error, the
// failure must NOT paint the user's terminal — it is rerouted into
// the per-task agent.log instead. tasks.Notify must still return
// normally so the FSM advance is not blocked, and the call sequence
// (issue → states → issueUpdate → reminder) is unchanged.
func TestLinearStateSync_RemindFails_LogsToAgentLogButTransitionSucceeds(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	env.remindErrors = []string{"down"}
	saveLinearAPIKey(t, "lin_api_TEST")
	logPath := agentLogPathOnlyDir(t)

	lifecycle.InitLinearStateSync()
	fireStateSyncTransitionWithLog("task-1", "ENG-1", logPath,
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
	logged := readAgentLog(t, logPath)
	if !strings.Contains(logged, "linear reminder_failed") {
		t.Fatalf("agent.log = %q, want reminder_failed marker",
			logged)
	}
	if !strings.Contains(logged, "issue=node-1") {
		t.Fatalf("agent.log = %q, want issue=node-1", logged)
	}
	if !strings.Contains(logged, "error=") {
		t.Fatalf("agent.log = %q, want error=...", logged)
	}
	msg := env.stderrText(t)
	if strings.Contains(msg, "issueReminder") {
		t.Fatalf("stderr = %q, want no issueReminder leak", msg)
	}
	if strings.Contains(msg, "linear sync: issueReminder") {
		t.Fatalf("stderr = %q, want no linear sync leak", msg)
	}
}
