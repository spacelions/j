package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_VerifyBegin_PostsPRLinkMention pins the new
// acceptance criterion: when a task transitions into verifying via
// EventVerifyBegin and has a non-empty PullRequestURL, the hook
// updates the workflow state AND posts a `@<viewer> <PR URL>`
// mention comment so reviewers see the PR link.
func TestLinearStateSync_VerifyBegin_PostsPRLinkMention(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransitionWithPR("task-1", "ENG-1",
		"https://github.com/spacelions/j/pull/42",
		tasks.StatusWorkDone, tasks.StatusVerifying,
		tasks.EventVerifyBegin)

	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "viewer", "commentCreate",
	}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if v := decodeMutationVar(t, got[2], "stateId"); v != "s-prog" {
		t.Fatalf("issueUpdate stateId = %q, want s-prog", v)
	}
	wantBody := "@user-uuid https://github.com/spacelions/j/pull/42"
	if v := decodeMutationVar(t, got[4], "body"); v != wantBody {
		t.Fatalf("commentCreate body = %q, want %q", v, wantBody)
	}
}
