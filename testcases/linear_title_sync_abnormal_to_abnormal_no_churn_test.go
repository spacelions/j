package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearTitleSync_AbnormalToAbnormal_NoChurn pins
// the "transitions that change the abnormal flavour
// (e.g. needs-clarification → failed) MUST keep ❗ — the
// prefix already applied is correct, no churn beyond
// that" rule. The hook still runs (LinearIssue is set,
// API key configured) so a GetIssue lookup is expected,
// but no issueUpdate write must follow because the
// computed title equals the existing one.
func TestLinearTitleSync_AbnormalToAbnormal_NoChurn(
	t *testing.T,
) {
	env := newLinearStateSyncEnv(t)
	env.issueResp.Title = "❗ Build pipeline"
	saveLinearAPIKey(t, "lin_api_TEST")
	lifecycle.InitLinearTitleSync()

	fireStateSyncTransition("task-1", "ENG-1",
		tasks.StatusNeedsClarification,
		tasks.StatusFailed, tasks.EventVerifyStuck)

	bodies := env.recordedBodies()
	for _, b := range bodies {
		if strings.Contains(b, "issueUpdate") {
			t.Fatalf("unexpected title rewrite: %s", b)
		}
	}
}
