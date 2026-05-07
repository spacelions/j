package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestLinearPush_HappyPath_PostsBoth pins the primary acceptance
// criterion: a Linear-sourced task whose plan transition succeeds
// triggers exactly one issueUpdate (carrying requirements.md
// byte-for-byte) and one commentCreate (carrying plan.md
// byte-for-byte) against the upstream Linear issue.
func TestLinearPush_HappyPath_PostsBoth(t *testing.T) {
	id := tasks.NewTaskID()
	const reqBody = "REQ payload\nline2\n"
	const planBody = "PLAN payload\nline2\n"
	env := newLinearPushEnv(t, id, reqBody, planBody)
	saveLinearAPIKey(t, "lin_api_TEST")

	lifecycle.InitLinearPush()
	firePlanDone(id, "ENG-1", tasks.EventPlanDone)

	got := env.recordedBodies()
	if len(got) != 3 {
		t.Fatalf("want 3 calls (issue,update,comment), got %d: %v",
			len(got), got)
	}
	if !strings.Contains(got[0], "issue(id:") {
		t.Fatalf("call[0] not issue lookup: %s", got[0])
	}
	if !strings.Contains(got[1], "issueUpdate") {
		t.Fatalf("call[1] not issueUpdate: %s", got[1])
	}
	if !strings.Contains(got[2], "commentCreate") {
		t.Fatalf("call[2] not commentCreate: %s", got[2])
	}
	if v := decodeMutationVar(t, got[1], "id"); v != "node-1" {
		t.Fatalf("issueUpdate id = %q, want node-1", v)
	}
	if v := decodeMutationVar(t, got[1], "body"); v != reqBody {
		t.Fatalf("issueUpdate body = %q, want %q", v, reqBody)
	}
	if v := decodeMutationVar(t, got[2], "id"); v != "node-1" {
		t.Fatalf("commentCreate id = %q, want node-1", v)
	}
	if v := decodeMutationVar(t, got[2], "body"); v != planBody {
		t.Fatalf("commentCreate body = %q, want %q", v, planBody)
	}
}
