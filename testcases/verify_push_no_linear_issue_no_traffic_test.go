package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearVerifyPush_NoLinearIssue_NoTraffic pins acceptance
// criterion #3: terminal verify transitions for tasks without a
// linked Linear issue must be no-ops (no panic, no FSM mutation, no
// HTTP traffic).
func TestLinearVerifyPush_NoLinearIssue_NoTraffic(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushAcceptanceEnv(t, id, "findings body")
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearVerifyPush()
	fireVerifyTerminal(
		id, "", tasks.StatusCompleted, tasks.EventVerifyPass)

	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %v", got)
	}
}
