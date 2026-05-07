package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearVerifyPush_MissingFindings_WarnsAndSkips pins
// acceptance criterion #3: when verifier_findings.md is missing,
// the verify-push hook must warn ("linear verify push:" prefix)
// and skip — no HTTP traffic, no FSM mutation, no panic.
func TestLinearVerifyPush_MissingFindings_WarnsAndSkips(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushAcceptanceEnv(t, id, "")
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearVerifyPush()
	fireVerifyTerminal(
		id, "ENG-1", tasks.StatusCompleted, tasks.EventVerifyPass)

	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %v", got)
	}
	msg := env.stderrText(t)
	if !strings.Contains(msg, tasks.VerifierFindingsFileName) {
		t.Fatalf("stderr = %q, want findings filename warning", msg)
	}
	if !strings.Contains(msg, "linear verify push") {
		t.Fatalf("stderr = %q, want 'linear verify push:' prefix", msg)
	}
}
