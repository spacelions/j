package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_PreserveUserSuffix pins the
// "if the user has manually edited the title (e.g.
// removed the prefix or appended text after the
// original title), the system MUST still set the
// correct prefix for the current status without
// otherwise mangling the user's edits — i.e., strip any
// leading ❗/👀 (with surrounding whitespace), then
// prepend the prefix dictated by the current status,
// leaving the rest of the title intact" rule. The
// suffix the user appended after the original title
// must round-trip through the rewrite verbatim.
func TestLinearTitleSync_PreserveUserSuffix(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	env.issueResp.Title = "❗ Build pipeline (do this first!)"
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusFailed, tasks.StatusWorking,
		tasks.EventWorkResume)

	bodies := env.recordedBodies()
	if len(bodies) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(bodies), bodies)
	}
	want := "Build pipeline (do this first!)"
	if got := decodeMutationVar(t, bodies[1], "title"); got != want {
		t.Fatalf("title var = %q, want %q", got, want)
	}
}
