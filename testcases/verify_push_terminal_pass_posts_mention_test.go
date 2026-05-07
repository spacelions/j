package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearVerifyPush_TerminalPass_PostsMention pins acceptance
// criterion #2: the verifying→completed transition (EventVerifyPass)
// must mirror verifier_findings.md to the linked Linear issue as a
// `@<viewer> Verification passed` mention comment.
func TestLinearVerifyPush_TerminalPass_PostsMention(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushAcceptanceEnv(t, id, "findings body")
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearVerifyPush()
	fireVerifyTerminal(
		id, "ENG-1", tasks.StatusCompleted, tasks.EventVerifyPass)

	got := env.recordedBodies()
	if len(got) != 3 {
		t.Fatalf(
			"want 3 calls (issue, viewer, commentCreate), got %d: %v",
			len(got), got)
	}
	if !strings.Contains(got[2], "commentCreate") {
		t.Fatalf("third call not commentCreate: %s", got[2])
	}
	body := decodeMutationVar(t, got[2], "body")
	want := "@user-uuid Verification passed\n\nfindings body"
	if body != want {
		t.Fatalf("commentCreate body = %q, want %q", body, want)
	}
}
