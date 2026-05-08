package testcases_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestVerifierSPA48_RemindFails_WritesToAgentLog pins acceptance
// criterion A: when RemindOnIssue returns a non-nil error and
// task.AgentLogPath is set, the file at task.AgentLogPath gains a
// new line carrying the issue id and the wrapped error, prefixed
// with an RFC3339Z timestamp. The hook never blocks the FSM.
func TestVerifierSPA48_RemindFails_WritesToAgentLog(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	env.remindErrors = []string{"Snooze date must be in the future"}
	saveLinearAPIKey(t, "lin_api_TEST")
	logPath := agentLogPathOnlyDir(t)

	lifecycle.InitLinearStateSync()
	fireStateSyncTransitionWithLog("task-1", "ENG-1", logPath,
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)

	got := readAgentLog(t, logPath)
	tsLine := regexp.MustCompile(
		`(?m)^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`)
	if !tsLine.MatchString(got) {
		t.Fatalf("agent.log = %q, want RFC3339Z prefix", got)
	}
	if !strings.Contains(got, "issue=node-1") {
		t.Fatalf("agent.log = %q, want issue=node-1", got)
	}
	if !strings.Contains(got, "error=") {
		t.Fatalf("agent.log = %q, want error=...", got)
	}
	if !strings.Contains(got, "Snooze date must be in the future") {
		t.Fatalf(
			"agent.log = %q, want wrapped Linear error", got)
	}
}
