package testcases_test

import (
	"io"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearVerifyPush_IterationFail_PostsPlainComment pins the
// per-iteration leg: each FAIL iteration of the verifier loop posts
// exactly one plain comment whose header is
// `Verification iteration N/M failed` followed by
// verifier_findings.md inline. The 1-based rendering (iteration 2/3
// from a 0-based iteration index of 1) is part of the contract; no
// `@<viewer>` prefix and no issueRemindMe round-trip.
func TestLinearVerifyPush_IterationFail_PostsPlainComment(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushAcceptanceEnv(t, id, "iter findings")
	saveLinearAPIKey(t, "lin_api_TEST")

	task := tasks.Task{ID: id, LinearIssue: "ENG-1"}
	lifecycle.PushVerifyIterationFinding(io.Discard, task, 1, 3)

	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(got), got)
	}
	body := decodeMutationVar(t, got[1], "body")
	want := "Verification iteration 2/3 failed" +
		"\n\niter findings"
	if body != want {
		t.Fatalf("commentCreate body = %q, want %q", body, want)
	}
	for _, b := range got {
		if strings.Contains(b, "issueRemindMe") {
			t.Fatalf("unexpected issueRemindMe: %v", got)
		}
	}
	if msg := env.stderrText(t); strings.Contains(msg, "linear verify") {
		t.Fatalf("unexpected stderr warning: %q", msg)
	}
}
