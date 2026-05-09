package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_DoubledPrefixCollapses guards the
// "strip any leading ❗/👀 (with surrounding whitespace)"
// rule against accumulated decorations. If, somehow,
// the upstream title carries a doubled "❗ ❗ Foo"
// (manual edit or a prior bug), a transition back to a
// normal status MUST collapse it all the way down to
// the bare title — not leave a residual ❗.
func TestLinearTitleSync_DoubledPrefixCollapses(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	env.issueResp.Title = "❗ ❗ Build pipeline"
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusFailed, tasks.StatusWorking,
		tasks.EventWorkResume)

	bodies := env.recordedBodies()
	if len(bodies) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(bodies), bodies)
	}
	if got := decodeMutationVar(t, bodies[1], "title"); got !=
		"Build pipeline" {
		t.Fatalf("title var = %q, want %q", got, "Build pipeline")
	}
}
