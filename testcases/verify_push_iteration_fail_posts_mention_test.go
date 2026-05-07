package testcases_test

import (
	"io"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearVerifyPush_IterationFail_PostsMention pins acceptance
// criterion #2 (per-iteration leg): each FAIL iteration of the
// verifier loop must post exactly one `@<viewer> Verification
// iteration N/M failed` mention comment with verifier_findings.md
// inline. The 1-based rendering (iteration 2/3 from a 0-based
// iteration index of 1) is part of the acceptance contract.
func TestLinearVerifyPush_IterationFail_PostsMention(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushAcceptanceEnv(t, id, "iter findings")
	saveLinearAPIKey(t, "lin_api_TEST")

	task := tasks.Task{ID: id, LinearIssue: "ENG-1"}
	lifecycle.PushVerifyIterationFinding(io.Discard, task, 1, 3)

	got := env.recordedBodies()
	if len(got) != 3 {
		t.Fatalf("want 3 calls, got %d: %v", len(got), got)
	}
	body := decodeMutationVar(t, got[2], "body")
	want := "@user-uuid Verification iteration 2/3 failed" +
		"\n\niter findings"
	if body != want {
		t.Fatalf("commentCreate body = %q, want %q", body, want)
	}
	if msg := env.stderrText(t); strings.Contains(msg, "linear verify") {
		t.Fatalf("unexpected stderr warning: %q", msg)
	}
}
