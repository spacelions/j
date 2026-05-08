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

func TestLinearStateSync_PlanDone_PostsTodoReminder(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-todo")
	assertVarStr(t, got[3], "id", "node-1")
}

func TestLinearStateSync_PlanPendingApproval_PostsTodoReminder(
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
		"issue", "states", "issueUpdate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-todo")
}

func TestLinearStateSync_Working_PostsInProgressNoPing(t *testing.T) {
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

func TestLinearStateSync_Verifying_PostsInProgressNoPing(t *testing.T) {
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

func TestLinearStateSync_Completed_PostsInReviewReminder(
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
		"issue", "states", "issueUpdate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-rev")
	assertVarStr(t, got[3], "id", "node-1")
}

func TestLinearStateSync_PlanRestart_PostsTodoNoReminder(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusWorking, tasks.StatusPlanning,
		tasks.EventPlanRestart)
	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-todo")
}

func TestLinearStateSync_PlanResume_PostsTodoNoReminder(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusFailed, tasks.StatusPlanning,
		tasks.EventPlanResume)
	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-todo")
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

func TestLinearStateSync_UpdateFails_StillPostsReminder(
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
		"issue", "states", "issueUpdate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "issueUpdate") {
		t.Fatalf("stderr = %q, want issueUpdate warning", msg)
	}
}

func TestLinearStateSync_RemindFails_Warns(t *testing.T) {
	env := newStateSyncEnv(t)
	env.remindErrors = []string{"down"}
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	if msg := env.stderrText(t); !strings.Contains(
		msg, "issueReminder") {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearStateSync_VerifyBegin_WithPR_PostsCommentAndReminder(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSyncWithPR("task-1", "ENG-1",
		"https://github.com/x/y/pull/42",
		tasks.StatusWorkDone, tasks.StatusVerifying,
		tasks.EventVerifyBegin)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "commentCreate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-prog")
	assertVarStr(t, got[3], "body",
		"https://github.com/x/y/pull/42")
	assertVarStr(t, got[4], "id", "node-1")
}

func TestLinearStateSync_VerifyRestart_WithPR_NoCommentNoReminder(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSyncWithPR("task-1", "ENG-1",
		"https://github.com/x/y/pull/42",
		tasks.StatusWorkDone, tasks.StatusVerifying,
		tasks.EventVerifyRestart)
	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-prog")
}

func TestLinearStateSync_VerifyBegin_PR_UpdateFails_StillPosts(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	env.updateErrors = []string{"boom"}
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSyncWithPR("task-1", "ENG-1",
		"https://github.com/x/y/pull/42",
		tasks.StatusWorkDone, tasks.StatusVerifying,
		tasks.EventVerifyBegin)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "commentCreate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "issueUpdate") {
		t.Fatalf("stderr = %q, want issueUpdate warning", msg)
	}
}

func TestLinearStateSync_VerifyBegin_PR_CommentFails_StillReminds(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	env.commentErrs = []string{"down"}
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSyncWithPR("task-1", "ENG-1",
		"https://github.com/x/y/pull/42",
		tasks.StatusWorkDone, tasks.StatusVerifying,
		tasks.EventVerifyBegin)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "commentCreate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "commentCreate") {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearStateSync_NeedsClarification_PlanReaper_PostsCommentAndReminder(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	logPath := writeClarification(t, "please clarify foo")
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", logPath,
		tasks.StatusPlanning, tasks.StatusNeedsClarification,
		tasks.EventReaperPlanNeedsClarification)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "commentCreate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-prog")
	assertVarStr(t, got[3], "body", "please clarify foo")
	assertVarStr(t, got[4], "id", "node-1")
}

func TestLinearStateSync_NeedsClarification_WorkReaper_PostsCommentAndReminder(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	logPath := writeClarification(t, "please clarify foo")
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", logPath,
		tasks.StatusWorking, tasks.StatusNeedsClarification,
		tasks.EventReaperWorkNeedsClarification)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "commentCreate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-prog")
	assertVarStr(t, got[3], "body", "please clarify foo")
	assertVarStr(t, got[4], "id", "node-1")
}

func TestLinearStateSync_NeedsClarification_VerifyReaper_PostsCommentAndReminder(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	logPath := writeClarification(t, "please clarify foo")
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", logPath,
		tasks.StatusVerifying, tasks.StatusNeedsClarification,
		tasks.EventReaperVerifyNeedsClarification)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "commentCreate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-prog")
	assertVarStr(t, got[3], "body", "please clarify foo")
	assertVarStr(t, got[4], "id", "node-1")
}

func TestLinearStateSync_NeedsClarification_NoAgentLogPath_StillRemindsAndWarns(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", "",
		tasks.StatusPlanning, tasks.StatusNeedsClarification,
		tasks.EventReaperPlanNeedsClarification)
	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate", "reminder"}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "no agent log path") {
		t.Fatalf("stderr = %q, want no-agent-log-path warning", msg)
	}
}

func TestLinearStateSync_NeedsClarification_FileMissing_StillReminds(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	logPath := agentLogPathOnly(t)
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", logPath,
		tasks.StatusPlanning, tasks.StatusNeedsClarification,
		tasks.EventReaperPlanNeedsClarification)
	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate", "reminder"}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "clarification.md") {
		t.Fatalf("stderr = %q, want clarification.md warning", msg)
	}
}

func TestLinearStateSync_NeedsClarification_FileEmpty_StillReminds(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	logPath := writeClarification(t, "   \n\t\n")
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", logPath,
		tasks.StatusPlanning, tasks.StatusNeedsClarification,
		tasks.EventReaperPlanNeedsClarification)
	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate", "reminder"}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	if msg := env.stderrText(t); !strings.Contains(msg, "empty") {
		t.Fatalf("stderr = %q, want empty-body warning", msg)
	}
}

func TestLinearStateSync_NeedsClarification_CommentFails_StillReminds(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	env.commentErrs = []string{"down"}
	saveAPIKey(t, "lin_api_test")
	logPath := writeClarification(t, "please clarify foo")
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", logPath,
		tasks.StatusPlanning, tasks.StatusNeedsClarification,
		tasks.EventReaperPlanNeedsClarification)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "commentCreate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "commentCreate") {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearStateSync_NeedsClarification_UpdateFails_StillPosts(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	env.updateErrors = []string{"boom"}
	saveAPIKey(t, "lin_api_test")
	logPath := writeClarification(t, "please clarify foo")
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", logPath,
		tasks.StatusPlanning, tasks.StatusNeedsClarification,
		tasks.EventReaperPlanNeedsClarification)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "commentCreate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "issueUpdate") {
		t.Fatalf("stderr = %q, want issueUpdate warning", msg)
	}
}

func TestLinearStateSync_NeedsClarification_RemindFails_Warns(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	env.remindErrors = []string{"down"}
	saveAPIKey(t, "lin_api_test")
	logPath := writeClarification(t, "please clarify foo")
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", logPath,
		tasks.StatusPlanning, tasks.StatusNeedsClarification,
		tasks.EventReaperPlanNeedsClarification)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "commentCreate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "issueReminder") {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearStateSync_NeedsClarification_NonReaperEvent_NoCommentNoReminder(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	logPath := writeClarification(t, "please clarify foo")
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", logPath,
		tasks.StatusPlanning, tasks.StatusNeedsClarification,
		tasks.EventPlanDone)
	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-prog")
}

func TestLinearStateSync_PlanResumeFromNeedsClarification_NoCommentNoReminder(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	logPath := writeClarification(t, "please clarify foo")
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", logPath,
		tasks.StatusNeedsClarification, tasks.StatusPlanning,
		tasks.EventPlanResume)
	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-todo")
}

func TestLinearStateSync_WorkResumeFromNeedsClarification_NoCommentNoReminder(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	logPath := writeClarification(t, "please clarify foo")
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", logPath,
		tasks.StatusNeedsClarification, tasks.StatusWorking,
		tasks.EventWorkResume)
	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-prog")
}

func TestLinearStateSync_VerifyResumeFromNeedsClarification_NoCommentNoReminder(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	logPath := writeClarification(t, "please clarify foo")
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", logPath,
		tasks.StatusNeedsClarification, tasks.StatusVerifying,
		tasks.EventVerifyResume)
	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-prog")
}

// TestLinearStateSync_NeedsClarification_ForegroundPlan_PostsCommentAndReminder
// pins the foreground planner-exit edge: the new event mirrors the
// reaper-driven cases above, so the call order is identical and the
// comment body matches the on-disk clarification.md content.
func TestLinearStateSync_NeedsClarification_ForegroundPlan_PostsCommentAndReminder(
	t *testing.T,
) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	logPath := writeClarification(t, "please clarify foo")
	InitLinearStateSync()
	fireStateSyncWithLog("task-1", "ENG-1", logPath,
		tasks.StatusPlanning, tasks.StatusNeedsClarification,
		tasks.EventPlanNeedsClarification)
	got := env.recordedBodies()
	want := []string{
		"issue", "states", "issueUpdate", "commentCreate", "reminder",
	}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	assertVarStr(t, got[2], "stateId", "s-prog")
	assertVarStr(t, got[3], "body", "please clarify foo")
	assertVarStr(t, got[4], "id", "node-1")
}
