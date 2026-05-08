package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_NeedsClarification_EmptyBody_StillReminds pins
// rule #4 for the whitespace-only body branch: when clarification.md
// exists but contains only whitespace, the comment is skipped with
// an `empty` warning but the inbox reminder still fires.
func TestLinearStateSync_NeedsClarification_EmptyBody_StillReminds(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")
	logPath := writeClarificationDir(t, "   \n\t\n")

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
	if msg := env.stderrText(t); !strings.Contains(msg, "empty") {
		t.Fatalf("stderr = %q, want empty-body warning", msg)
	}
}
