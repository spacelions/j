package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearVerifyPush_TerminalFail_PostsPlainComment pins the
// "verify-fail mirrors findings to Linear without an inbox ping"
// acceptance criterion: EventVerifyFail posts verifier_findings.md
// as a plain comment with the
// "Verification failed (retries exhausted)" header and does NOT
// call issueRemindMe.
func TestLinearVerifyPush_TerminalFail_PostsPlainComment(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushAcceptanceEnv(t, id, "findings body")
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearVerifyPush()
	fireVerifyTerminal(
		id, "ENG-1", tasks.StatusFailed, tasks.EventVerifyFail)

	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[1], "commentCreate") {
		t.Fatalf("second call not commentCreate: %s", got[1])
	}
	body := decodeMutationVar(t, got[1], "body")
	want := "Verification failed (retries exhausted)" +
		"\n\nfindings body"
	if body != want {
		t.Fatalf("commentCreate body = %q, want %q", body, want)
	}
	for _, b := range got {
		if strings.Contains(b, "issueRemindMe") {
			t.Fatalf("unexpected issueRemindMe: %v", got)
		}
	}
}
