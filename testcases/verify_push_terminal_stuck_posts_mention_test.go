package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearVerifyPush_TerminalStuck_PostsMention pins the
// EventVerifyStuck leg of acceptance criterion #2: the
// reaper-driven verifying→failed transition must mirror the same
// "Verification failed (retries exhausted)" comment as
// EventVerifyFail.
func TestLinearVerifyPush_TerminalStuck_PostsMention(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushAcceptanceEnv(t, id, "findings body")
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearVerifyPush()
	fireVerifyTerminal(
		id, "ENG-1", tasks.StatusFailed, tasks.EventVerifyStuck)

	got := env.recordedBodies()
	if len(got) != 3 {
		t.Fatalf("want 3 calls, got %d: %v", len(got), got)
	}
	body := decodeMutationVar(t, got[2], "body")
	if !strings.HasPrefix(
		body,
		"@user-uuid Verification failed (retries exhausted)",
	) {
		t.Fatalf("commentCreate body = %q", body)
	}
	if !strings.Contains(body, "findings body") {
		t.Fatalf("commentCreate body missing findings: %q", body)
	}
}
