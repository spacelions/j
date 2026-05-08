package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestVerifierSPA48_OtherWarnings_StillStderr pins acceptance
// criterion D: SPA-48 only re-routes the issueReminder failure
// log — every other warnLinearSync(...) branch keeps its existing
// stderr DangerousDialogBox behaviour. We exercise the
// issueUpdate failure branch and assert the `linear sync:` prefix
// still surfaces on the redirected stderr pipe.
func TestVerifierSPA48_OtherWarnings_StillStderr(t *testing.T) {
	env := newLinearStateSyncEnv(t)
	env.updateErrors = []string{"boom"}
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearStateSync()
	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)

	msg := env.stderrText(t)
	if !strings.Contains(msg, "linear sync") {
		t.Fatalf("stderr = %q, want 'linear sync' warning", msg)
	}
	if !strings.Contains(msg, "issueUpdate") {
		t.Fatalf("stderr = %q, want issueUpdate warning", msg)
	}
}
