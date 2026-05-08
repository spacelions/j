package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearVerifyPush_TerminalPass_PostsPlainComment pins the
// "verify-pass mirrors findings to Linear without an inbox ping"
// acceptance criterion: the verifying→completed transition
// (EventVerifyPass) posts verifier_findings.md as a plain comment
// on the linked Linear issue with a `Verification passed` header,
// and does NOT call issueReminder (the comment is for context, not
// a page).
func TestLinearVerifyPush_TerminalPass_PostsPlainComment(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushAcceptanceEnv(t, id, "findings body")
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearVerifyPush()
	fireVerifyTerminal(
		id, "ENG-1", tasks.StatusCompleted, tasks.EventVerifyPass)

	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf(
			"want 2 calls (issue, commentCreate), got %d: %v",
			len(got), got)
	}
	if !strings.Contains(got[1], "commentCreate") {
		t.Fatalf("second call not commentCreate: %s", got[1])
	}
	body := decodeMutationVar(t, got[1], "body")
	want := "Verification passed\n\nfindings body"
	if body != want {
		t.Fatalf("commentCreate body = %q, want %q", body, want)
	}
	for _, b := range got {
		if strings.Contains(b, "issueReminder") {
			t.Fatalf("unexpected issueReminder: %v", got)
		}
	}
}
