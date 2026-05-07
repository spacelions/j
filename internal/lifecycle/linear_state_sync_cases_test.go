package lifecycle

import (
	"os"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

func TestLinearStateSync_NoLinearIssue_NoHTTP(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no traffic, got %v", got)
	}
}

func TestLinearStateSync_UnmappedStatus_NoHTTP(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusHelp,
		tasks.EventPlanError)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no traffic, got %v", got)
	}
}

func TestLinearStateSync_PlanDone_PostsTodoMention(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "viewer", "commentCreate",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-todo")
	assertVarStr(t, got[4], "body", "@user-uuid todo")
}

func TestLinearStateSync_PlanPendingApproval_PostsTodo(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanPendingApproval,
		tasks.EventPlanAwaitApproval)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "viewer", "commentCreate",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-todo")
}

func TestLinearStateSync_Working_PostsInProgress(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanDone, tasks.StatusWorking,
		tasks.EventWorkBegin)
	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-prog")
}

func TestLinearStateSync_Verifying_PostsInProgress(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusWorkDone, tasks.StatusVerifying,
		tasks.EventVerifyBegin)
	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-prog")
}

func TestLinearStateSync_Completed_PostsInReviewMention(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusVerifying, tasks.StatusCompleted,
		tasks.EventVerifyPass)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "viewer", "commentCreate",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-rev")
	assertVarStr(t, got[4], "body", "@user-uuid todo")
}

func TestLinearStateSync_StateNameMissing_Warns(t *testing.T) {
	env := newStateSyncEnv(t)
	env.states = []linear.WorkflowState{
		{ID: "s-other", Name: "Triage", Type: "backlog"},
	}
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	got := env.recordedBodies()
	for _, b := range got {
		if strings.Contains(b, "issueUpdate") {
			t.Fatalf("issueUpdate sent despite missing state: %v", got)
		}
	}
	if msg := env.stderrText(t); !strings.Contains(msg, "Todo") {
		t.Fatalf("stderr = %q, want missing-state warning", msg)
	}
}

func TestLinearStateSync_NoAPIKey_Warns(t *testing.T) {
	env := newStateSyncEnv(t)
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no traffic, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(msg, "linear sync") {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearStateSync_LoadAPIKeyError_Warns(t *testing.T) {
	env := newStateSyncEnv(t)
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no traffic, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "load api key") {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearStateSync_GetIssueFails_Warns(t *testing.T) {
	env := newStateSyncEnv(t)
	env.issueResp = nil
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	got := env.recordedBodies()
	if len(got) != 1 || !strings.Contains(got[0], "issue(id:") {
		t.Fatalf("expected one issue lookup only, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(msg, "resolve") {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearStateSync_ListStatesFails_Warns(t *testing.T) {
	env := newStateSyncEnv(t)
	env.statesErrors = []string{"boom"}
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	got := env.recordedBodies()
	for _, b := range got {
		if strings.Contains(b, "issueUpdate") {
			t.Fatalf("issueUpdate sent despite list-states fail: %v",
				got)
		}
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "list states") {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearStateSync_UpdateFails_StillPostsComment(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	env.updateErrors = []string{"boom"}
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "viewer", "commentCreate",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "issueUpdate") {
		t.Fatalf("stderr = %q, want issueUpdate warning", msg)
	}
}

func TestLinearStateSync_ViewerFails_Warns(t *testing.T) {
	env := newStateSyncEnv(t)
	env.viewerErrors = []string{"nope"}
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	got := env.recordedBodies()
	for _, b := range got {
		if strings.Contains(b, "commentCreate") {
			t.Fatalf("commentCreate sent despite viewer fail: %v",
				got)
		}
	}
	if msg := env.stderrText(t); !strings.Contains(msg, "viewer") {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearStateSync_CommentFails_Warns(t *testing.T) {
	env := newStateSyncEnv(t)
	env.commentErrs = []string{"down"}
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	if msg := env.stderrText(t); !strings.Contains(
		msg, "commentCreate") {
		t.Fatalf("stderr = %q", msg)
	}
}
