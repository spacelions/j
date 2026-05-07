package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearVerifyPush_TerminalFail_PostsMention pins acceptance
// criterion #2 (terminal failure leg): the verifying→failed
// transition driven by EventVerifyFail must mirror
// verifier_findings.md to the linked Linear issue with the
// "Verification failed (retries exhausted)" header.
func TestLinearVerifyPush_TerminalFail_PostsMention(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushAcceptanceEnv(t, id, "findings body")
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearVerifyPush()
	fireVerifyTerminal(
		id, "ENG-1", tasks.StatusFailed, tasks.EventVerifyFail)

	got := env.recordedBodies()
	if len(got) != 3 {
		t.Fatalf("want 3 calls, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[2], "commentCreate") {
		t.Fatalf("third call not commentCreate: %s", got[2])
	}
	body := decodeMutationVar(t, got[2], "body")
	want := "@user-uuid Verification failed (retries exhausted)" +
		"\n\nfindings body"
	if body != want {
		t.Fatalf("commentCreate body = %q, want %q", body, want)
	}
}
