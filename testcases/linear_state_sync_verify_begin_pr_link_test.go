package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_VerifyBegin_PostsPRLinkAndReminds pins the
// "verify-begin with a PR URL" acceptance criterion: when a task
// transitions into verifying via EventVerifyBegin and has a
// non-empty PullRequestURL, the hook updates the workflow state to
// "In Progress", posts the PR URL as a plain comment, and schedules
// a Linear inbox reminder so the API-key owner is paged with a
// click-through path to the PR. The comment is plain (no @-mention)
// because Linear suppresses self-mention notifications anyway; the
// reminder is what surfaces the inbox entry.
func TestLinearStateSync_VerifyBegin_PostsPRLinkAndReminds(
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
		"issue", "states", "issueUpdate", "commentCreate", "reminder",
	}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	if v := decodeMutationVar(t, got[2], "stateId"); v != "s-prog" {
		t.Fatalf("issueUpdate stateId = %q, want s-prog", v)
	}
	wantBody := "https://github.com/spacelions/j/pull/42"
	if v := decodeMutationVar(t, got[3], "body"); v != wantBody {
		t.Fatalf("commentCreate body = %q, want %q", v, wantBody)
	}
	if v := decodeMutationVar(t, got[4], "id"); v != "node-1" {
		t.Fatalf("issueReminder id = %q, want node-1", v)
	}
}
