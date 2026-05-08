package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_NeedsClarification_NoAgentLogPath_StillReminds
// pins the rule-#4 independence guarantee: an empty AgentLogPath
// skips the comment with a `clarification` warning, but the inbox
// reminder must still fire so the human is paged.
func TestLinearStateSync_NeedsClarification_NoAgentLogPath_StillReminds(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransitionWithLog("task-1", "ENG-1", "",
		tasks.StatusPlanning, tasks.StatusNeedsClarification,
		tasks.EventReaperPlanNeedsClarification)

	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "reminder",
	}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if v := decodeMutationVar(t, got[2], "stateId"); v != "s-prog" {
		t.Fatalf("issueUpdate stateId = %q, want s-prog", v)
	}
	if v := decodeMutationVar(t, got[3], "id"); v != "node-1" {
		t.Fatalf("issueReminder id = %q, want node-1", v)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "clarification") {
		t.Fatalf("stderr = %q, want clarification warning", msg)
	}
}
