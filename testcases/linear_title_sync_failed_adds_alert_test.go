package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_Failed_AddsAlert pins the
// "❗ prefix when the task enters failed" acceptance
// criterion. A plain title must gain a leading "❗ "
// (single space) on entry to StatusFailed via the
// verify-stuck event — verify-stuck is what the
// requirements call "stuck" and resolves to failed.
func TestLinearTitleSync_Failed_AddsAlert(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	env.issueResp.Title = "Build pipeline"
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusFailed,
		tasks.EventVerifyStuck)

	bodies := env.recordedBodies()
	if len(bodies) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(bodies), bodies)
	}
	if !strings.Contains(bodies[1], "issueUpdate") ||
		!strings.Contains(bodies[1], "title:$title") {
		t.Fatalf("expected title issueUpdate, got %s", bodies[1])
	}
	if got := decodeMutationVar(t, bodies[1], "title"); got !=
		"❗ Build pipeline" {
		t.Fatalf("title var = %q, want %q", got, "❗ Build pipeline")
	}
}
