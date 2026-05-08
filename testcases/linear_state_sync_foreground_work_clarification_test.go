package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_ForegroundWorkNeedsClarification pins acceptance
// criterion 4 from requirements.md: when the foreground worker emits
// `EventWorkNeedsClarification` and the row lands in
// `needs-clarification`, the linear-state-sync hook mirrors the
// reaper-driven branches — issue lookup, list states, issueUpdate
// (state → "In Progress"), commentCreate carrying the on-disk
// clarification.md byte-for-byte, and an inbox reminder.
func TestLinearStateSync_ForegroundWorkNeedsClarification(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_test")
	logPath := writeForegroundClarification(t, "answer me X")
	lifecycle.InitLinearStateSync()

	tasks.Notify(
		tasks.Transition{
			From:  tasks.StatusWorking,
			Event: tasks.EventWorkNeedsClarification,
			To:    tasks.StatusNeedsClarification,
		},
		tasks.Task{
			ID:           "task-fg-work",
			Status:       tasks.StatusNeedsClarification,
			LinearIssue:  "ENG-1",
			AgentLogPath: logPath,
		},
	)

	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "commentCreate", "reminder",
	}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKindList(got), want)
	}
	assertVarValue(t, got[2], "stateId", "s-prog")
	assertVarValue(t, got[3], "body", "answer me X")
	assertVarValue(t, got[4], "id", "node-1")
}
