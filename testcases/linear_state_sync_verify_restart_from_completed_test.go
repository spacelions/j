package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearStateSync_VerifyRestart_FromCompleted_NoComment pins the
// "first time only" guarantee for the completed → verifying restart
// path: even with a non-empty PullRequestURL, an EventVerifyRestart
// must update the workflow state but not post a mention comment.
func TestLinearStateSync_VerifyRestart_FromCompleted_NoComment(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransitionWithPR("task-1", "ENG-1",
		"https://github.com/spacelions/j/pull/42",
		tasks.StatusCompleted, tasks.StatusVerifying,
		tasks.EventVerifyRestart)

	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalSlices(bodyKindList(got), want) {
		t.Fatalf("call order = %v, want %v",
			bodyKindList(got), want)
	}
	for _, b := range got {
		if strings.Contains(b, "commentCreate") {
			t.Fatalf(
				"unexpected commentCreate on completed restart: %v",
				got)
		}
		if strings.Contains(b, "viewer{id") {
			t.Fatalf(
				"unexpected viewer fetch on completed restart: %v",
				got)
		}
	}
}
