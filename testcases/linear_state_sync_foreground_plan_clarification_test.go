package testcases_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_ForegroundPlanNeedsClarification pins
// acceptance criterion 6 from plan.md: when the foreground planner
// emits `EventPlanNeedsClarification` and the row lands in
// `needs-clarification`, the linear-state-sync hook must mirror the
// reaper-driven branches — issue lookup, list states, issueUpdate
// (state → "In Progress"), commentCreate carrying the on-disk
// clarification.md byte-for-byte, and an inbox reminder.
func TestLinearStateSync_ForegroundPlanNeedsClarification(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_test")
	logPath := writeForegroundClarification(t, "please clarify foo")
	lifecycle.InitLinearStateSync()

	tasks.Notify(
		tasks.Transition{
			From:  tasks.StatusPlanning,
			Event: tasks.EventPlanNeedsClarification,
			To:    tasks.StatusNeedsClarification,
		},
		tasks.Task{
			ID:           "task-fg",
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
	assertVarValue(t, got[3], "body", "please clarify foo")
	assertVarValue(t, got[4], "id", "node-1")
}

// writeForegroundClarification drops a clarification.md inside a
// fresh temp dir and returns an `agent.log` path inside that dir.
// The state-sync hook resolves taskDir as filepath.Dir(AgentLogPath).
func writeForegroundClarification(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "clarification.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write clarification.md: %v", err)
	}
	return filepath.Join(dir, "agent.log")
}

// assertVarValue decodes the GraphQL request body and asserts that
// `variables[key]` equals `want`. Used to pin the exact issueUpdate
// stateId, commentCreate body, and reminder targetId values.
func assertVarValue(t *testing.T, body, key, want string) {
	t.Helper()
	var req struct {
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode body: %v (%s)", err, body)
	}
	got, _ := req.Variables[key].(string)
	if got != want {
		t.Fatalf("variables[%q] = %q, want %q (body=%s)",
			key, got, want, strings.TrimSpace(body))
	}
}
