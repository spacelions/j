package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearVerifyPush_TerminalStuck_PostsPlainComment pins the
// EventVerifyStuck leg: the reaper-driven verifying→failed
// transition mirrors the same "Verification failed (retries
// exhausted)" plain-comment shape as EventVerifyFail and does NOT
// call issueReminder.
func TestLinearVerifyPush_TerminalStuck_PostsPlainComment(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushAcceptanceEnv(t, id, "findings body")
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearVerifyPush()
	fireVerifyTerminal(
		id, "ENG-1", tasks.StatusFailed, tasks.EventVerifyStuck)

	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(got), got)
	}
	body := decodeMutationVar(t, got[1], "body")
	if !strings.HasPrefix(
		body,
		"Verification failed (retries exhausted)",
	) {
		t.Fatalf("commentCreate body = %q", body)
	}
	if strings.HasPrefix(body, "@") {
		t.Fatalf("body unexpectedly starts with mention: %q", body)
	}
	if !strings.Contains(body, "findings body") {
		t.Fatalf("commentCreate body missing findings: %q", body)
	}
	for _, b := range got {
		if strings.Contains(b, "issueReminder") {
			t.Fatalf("unexpected issueReminder: %v", got)
		}
	}
}
