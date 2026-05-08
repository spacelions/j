package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_NeedsClarification_RemindFails_Warns pins
// rule #4 for a failing issueReminder: the call is attempted and
// fails, surfacing as an `issueReminder` warning on stderr.
func TestLinearStateSync_NeedsClarification_RemindFails_Warns(
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
	if msg := env.stderrText(t); !strings.Contains(
		msg, "issueReminder") {
		t.Fatalf("stderr = %q, want issueReminder warning", msg)
	}
}
