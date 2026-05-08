package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_NeedsClarification_FileMissing_StillReminds
// pins rule #4 for the missing-file branch: when AgentLogPath points
// at a directory without clarification.md, the comment is skipped
// with a `clarification.md` warning but the inbox reminder still
// fires.
func TestLinearStateSync_NeedsClarification_FileMissing_StillReminds(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")
	logPath := agentLogPathOnlyDir(t)

	lifecycle.InitLinearStateSync()
	fireStateSyncTransitionWithLog("task-1", "ENG-1", logPath,
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
	if v := decodeMutationVar(t, got[3], "id"); v != "node-1" {
		t.Fatalf("issueReminder id = %q, want node-1", v)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "clarification.md") {
		t.Fatalf("stderr = %q, want clarification.md warning",
			msg)
	}
}
