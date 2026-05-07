package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearPush_IssueUpdateFails_StillPostsComment pins "a failure
// of issueUpdate does not prevent commentCreate from being
// attempted". The two mutations are independent — re-plans converge,
// so a failed description update is recoverable next time, but the
// plan-comment record must not be skipped.
func TestLinearPush_IssueUpdateFails_StillPostsComment(t *testing.T) {
	id := tasks.NewTaskID()
	env := newLinearPushEnv(t, id, "req", "plan")
	env.updateErrors = []string{"validation: bad description"}
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearPush()
	firePlanDone(id, "ENG-1", tasks.EventPlanDone)

	got := env.recordedBodies()
	if len(got) != 3 {
		t.Fatalf("want 3 calls (issue,update,comment), got %d: %v",
			len(got), got)
	}
	if !strings.Contains(got[2], "commentCreate") {
		t.Fatalf("commentCreate not attempted after update fail: %v",
			got)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "issueUpdate") {
		t.Fatalf("stderr = %q, want issueUpdate warning", msg)
	}
}
