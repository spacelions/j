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

// assertSyncedTo asserts the recorded GraphQL traffic was exactly the
// three calls of a successful sync (issue resolve, list states, issue
// update) and that the issueUpdate carries the expected stateId.
func assertSyncedTo(t *testing.T, bodies []string, wantStateID string) {
	t.Helper()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalKinds(bodyKinds(bodies), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(bodies), want)
	}
	assertVarStr(t, bodies[2], "stateId", wantStateID)
}

func TestLinearStateSync_PlanDone_PostsTodo(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	assertSyncedTo(t, env.recordedBodies(), "s-todo")
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
	assertSyncedTo(t, env.recordedBodies(), "s-todo")
}

func TestLinearStateSync_PlanResume_PostsTodo(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanPendingApproval, tasks.StatusPlanning,
		tasks.EventPlanResume)
	assertSyncedTo(t, env.recordedBodies(), "s-todo")
}

func TestLinearStateSync_PlanRestart_PostsTodo(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusCompleted, tasks.StatusPlanning,
		tasks.EventPlanRestart)
	assertSyncedTo(t, env.recordedBodies(), "s-todo")
}

func TestLinearStateSync_Working_PostsInProgress(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanDone, tasks.StatusWorking,
		tasks.EventWorkBegin)
	assertSyncedTo(t, env.recordedBodies(), "s-prog")
}

func TestLinearStateSync_Verifying_PostsInProgress(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusWorkDone, tasks.StatusVerifying,
		tasks.EventVerifyBegin)
	assertSyncedTo(t, env.recordedBodies(), "s-prog")
}

func TestLinearStateSync_Completed_PostsInReview(t *testing.T) {
	env := newStateSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusVerifying, tasks.StatusCompleted,
		tasks.EventVerifyPass)
	assertSyncedTo(t, env.recordedBodies(), "s-rev")
}

// TestLinearStateSync_NoCommentCreate exercises every synced
// destination and asserts none of them ever issues a commentCreate
// mutation — the @-mention path was removed.
func TestLinearStateSync_NoCommentCreate(t *testing.T) {
	cases := []struct {
		name     string
		from, to tasks.TaskStatus
		ev       tasks.Event
	}{
		{"PlanDone", tasks.StatusPlanning,
			tasks.StatusPlanDone, tasks.EventPlanDone},
		{"PlanPendingApproval", tasks.StatusPlanning,
			tasks.StatusPlanPendingApproval,
			tasks.EventPlanAwaitApproval},
		{"Planning", tasks.StatusPlanDone,
			tasks.StatusPlanning, tasks.EventPlanRestart},
		{"Working", tasks.StatusPlanDone,
			tasks.StatusWorking, tasks.EventWorkBegin},
		{"Verifying", tasks.StatusWorkDone,
			tasks.StatusVerifying, tasks.EventVerifyBegin},
		{"Completed", tasks.StatusVerifying,
			tasks.StatusCompleted, tasks.EventVerifyPass},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newStateSyncEnv(t)
			saveAPIKey(t, "lin_api_test")
			InitLinearStateSync()
			fireStateSync("task-1", "ENG-1", tc.from, tc.to, tc.ev)
			for _, b := range env.recordedBodies() {
				if strings.Contains(b, "commentCreate") {
					t.Fatalf("commentCreate sent for %s: %s",
						tc.name, b)
				}
				if strings.Contains(b, "viewer{id") {
					t.Fatalf("viewer query sent for %s: %s",
						tc.name, b)
				}
			}
		})
	}
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

func TestLinearStateSync_UpdateFails_Warns(t *testing.T) {
	env := newStateSyncEnv(t)
	env.updateErrors = []string{"boom"}
	saveAPIKey(t, "lin_api_test")
	InitLinearStateSync()
	fireStateSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanDone,
		tasks.EventPlanDone)
	got := env.recordedBodies()
	want := []string{"issue", "states", "issueUpdate"}
	if !equalKinds(bodyKinds(got), want) {
		t.Fatalf("call order = %v, want %v", bodyKinds(got), want)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "issueUpdate") {
		t.Fatalf("stderr = %q, want issueUpdate warning", msg)
	}
}
