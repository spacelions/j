package lifecycle

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/tools/linear"
)

// pushTestEnv bundles the per-test scaffolding the linear-push hook
// tests reuse: a stubbed Linear endpoint with recorded mutations, a
// temporary cwd holding the per-task markdown files, and a redirected
// stderr buffer for warning assertions. Centralising it keeps each
// case to its own logical assertions.
type pushTestEnv struct {
	srv          *httptest.Server
	bodies       []string
	issueResp    *linear.Issue
	issueErrors  []string
	updateErrors []string
	commentErrs  []string
	mu           sync.Mutex
	stderrR      *os.File
	stderrW      *os.File
	stderrOrig   *os.File
	stderrClosed bool
}

// newPushEnv writes requirements.md / plan.md under the test cwd and
// installs an httptest.Server-backed Linear endpoint. The caller
// drives the recorded mutation responses by mutating issueResp /
// updateErrors / commentErrs before triggering the hook.
func newPushEnv(t *testing.T, taskID, req, plan string) *pushTestEnv {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	dir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if req != "" {
		writeFile(t,
			filepath.Join(dir, tasks.RequirementsFileName), req)
	}
	if plan != "" {
		writeFile(t, filepath.Join(dir, tasks.PlanFileName), plan)
	}
	env := &pushTestEnv{
		issueResp: &linear.Issue{
			ID: "node-1", Identifier: "ENG-1", Title: "t",
		},
	}
	env.srv = httptest.NewServer(
		http.HandlerFunc(env.handle))
	t.Cleanup(env.srv.Close)
	prev := linear.TestEndpoint
	linear.TestEndpoint = env.srv.URL
	t.Cleanup(func() { linear.TestEndpoint = prev })
	tasks.ResetHooksForTest()
	t.Cleanup(tasks.ResetHooksForTest)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	env.stderrOrig = os.Stderr
	os.Stderr = w
	env.stderrR = r
	env.stderrW = w
	t.Cleanup(func() {
		if !env.stderrClosed {
			_ = w.Close()
		}
		os.Stderr = env.stderrOrig
	})
	return env
}

// stderrText drains and returns whatever the hook wrote to stderr.
// Closes the writer end first so the read returns at EOF; subsequent
// hook firings after this call would block, so each test calls it
// at most once at the end.
func (e *pushTestEnv) stderrText(t *testing.T) string {
	t.Helper()
	os.Stderr = e.stderrOrig
	if !e.stderrClosed {
		_ = e.stderrW.Close()
		e.stderrClosed = true
	}
	buf, err := io.ReadAll(e.stderrR)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(buf)
}

func (e *pushTestEnv) recordedBodies() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.bodies))
	copy(out, e.bodies)
	return out
}

func (e *pushTestEnv) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	e.mu.Lock()
	e.bodies = append(e.bodies, string(body))
	e.mu.Unlock()
	q := string(body)
	switch {
	case strings.Contains(q, "issueUpdate"):
		writeMutation(w, "issueUpdate", e.updateErrors)
	case strings.Contains(q, "commentCreate"):
		writeMutation(w, "commentCreate", e.commentErrs)
	case strings.Contains(q, "issue(id:"):
		payload := map[string]any{
			"data": map[string]any{"issue": e.issueResp},
		}
		if len(e.issueErrors) > 0 {
			payload["errors"] = errorList(e.issueErrors)
		}
		writeJSON(w, payload)
	default:
		http.Error(w, "unknown query", http.StatusBadRequest)
	}
}

func writeMutation(
	w http.ResponseWriter, field string, errs []string,
) {
	payload := map[string]any{
		"data": map[string]any{
			field: map[string]any{"success": len(errs) == 0},
		},
	}
	if len(errs) > 0 {
		payload["errors"] = errorList(errs)
	}
	writeJSON(w, payload)
}

func errorList(msgs []string) []map[string]string {
	out := make([]map[string]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, map[string]string{"message": m})
	}
	return out
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// fireHook sends the registered hooks the planner-success transition
// for a Linear-sourced task. Helper to keep individual cases focused.
func fireHook(taskID, linearIssue string, ev tasks.Event) {
	tr := tasks.Transition{
		From: tasks.StatusPlanning, Event: ev,
		To: tasks.StatusPlanDone,
	}
	task := tasks.Task{
		ID: taskID, Status: tasks.StatusPlanDone,
		LinearIssue: linearIssue,
	}
	tasks.Notify(tr, task)
}

// fireHookTo dispatches a synthetic transition with an explicit `to`
// status so the linear-push defensive-guard branch can be exercised
// without coupling to fireHook's plan-done default.
func fireHookTo(
	taskID, linearIssue string, ev tasks.Event, to tasks.TaskStatus,
) {
	tasks.Notify(
		tasks.Transition{
			From: tasks.StatusPlanning, Event: ev, To: to,
		},
		tasks.Task{
			ID: taskID, Status: to, LinearIssue: linearIssue,
		},
	)
}

func saveAPIKey(t *testing.T, token string) {
	t.Helper()
	if err := linear.SaveAPIKey(token); err != nil {
		t.Fatalf("SaveAPIKey: %v", err)
	}
}

// ============================== cases ==============================

func TestLinearPush_NonLinearTask_NoHTTP(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "req", "plan")
	saveAPIKey(t, "lin_api_test")
	InitLinearPush()
	fireHook(id, "", tasks.EventPlanDone)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %d: %v", len(got), got)
	}
}

func TestLinearPush_NonPlanSuccessEvent_NoHTTP(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "req", "plan")
	saveAPIKey(t, "lin_api_test")
	InitLinearPush()
	fireHook(id, "ENG-1", tasks.EventPlanError)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %d: %v", len(got), got)
	}
}

func TestLinearPush_HappyPath_PostsBoth(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "req body", "plan body")
	saveAPIKey(t, "lin_api_test")
	InitLinearPush()
	fireHook(id, "ENG-1", tasks.EventPlanDone)
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
	assertMutationVar(t, got[1], "id", "node-1")
	assertMutationVar(t, got[1], "body", "req body")
	assertMutationVar(t, got[2], "id", "node-1")
	assertMutationVar(t, got[2], "body", "plan body")
}

func TestLinearPush_Replan_AddsNewComment(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "req", "plan v1")
	saveAPIKey(t, "lin_api_test")
	InitLinearPush()
	fireHook(id, "ENG-1", tasks.EventPlanDone)
	dir, _ := tasks.DefaultDir()
	writeFile(t, filepath.Join(dir, id, tasks.PlanFileName), "plan v2")
	fireHook(id, "ENG-1", tasks.EventReaperPlanDone)
	got := env.recordedBodies()
	commentCount := 0
	for _, body := range got {
		if strings.Contains(body, "commentCreate") {
			commentCount++
		}
	}
	if commentCount != 2 {
		t.Fatalf("want 2 commentCreate calls, got %d in %v",
			commentCount, got)
	}
}

func TestLinearPush_LoadAPIKeyError_Warns(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "req", "plan")
	// Replace the settings file with a directory so store.Open
	// fails — exercises the LoadAPIKey-returns-error branch
	// without exposing a test-only seam in the linear package.
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
	InitLinearPush()
	fireHook(id, "ENG-1", tasks.EventPlanDone)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "load api key") {
		t.Fatalf("stderr = %q, want 'load api key' warning", msg)
	}
}

func TestLinearPush_NoAPIKey_Warns(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "req", "plan")
	InitLinearPush()
	fireHook(id, "ENG-1", tasks.EventPlanDone)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(msg, "linear push") {
		t.Fatalf("stderr = %q, want a 'linear push' warning", msg)
	}
}

func TestLinearPush_GetIssueNotFound_Warns(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "req", "plan")
	env.issueResp = nil
	saveAPIKey(t, "lin_api_test")
	InitLinearPush()
	fireHook(id, "ENG-1", tasks.EventPlanDone)
	got := env.recordedBodies()
	if len(got) != 1 || !strings.Contains(got[0], "issue(id:") {
		t.Fatalf("expected one issue lookup only, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(msg, "linear push") {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearPush_UpdateFails_StillPostsComment(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "req", "plan")
	env.updateErrors = []string{"boom"}
	saveAPIKey(t, "lin_api_test")
	InitLinearPush()
	fireHook(id, "ENG-1", tasks.EventPlanDone)
	got := env.recordedBodies()
	if len(got) != 3 {
		t.Fatalf("want 3 calls, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[2], "commentCreate") {
		t.Fatalf("commentCreate not attempted after update fail: %v",
			got)
	}
	if msg := env.stderrText(t); !strings.Contains(msg, "issueUpdate") {
		t.Fatalf("stderr = %q, want issueUpdate warning", msg)
	}
}

func TestLinearPush_CommentFails_Warns(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "req", "plan")
	env.commentErrs = []string{"nope"}
	saveAPIKey(t, "lin_api_test")
	InitLinearPush()
	fireHook(id, "ENG-1", tasks.EventPlanDone)
	got := env.recordedBodies()
	if len(got) != 3 {
		t.Fatalf("want 3 calls, got %d: %v", len(got), got)
	}
	if msg := env.stderrText(t); !strings.Contains(msg, "commentCreate") {
		t.Fatalf("stderr = %q, want commentCreate warning", msg)
	}
}

func TestLinearPush_PlanReadError_NoHTTP(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "req", "")
	saveAPIKey(t, "lin_api_test")
	InitLinearPush()
	fireHook(id, "ENG-1", tasks.EventPlanDone)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(msg, "plan.md") {
		t.Fatalf("stderr = %q, want plan.md read error warning", msg)
	}
}

func TestLinearPush_RequirementsReadError_NoHTTP(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "", "plan")
	saveAPIKey(t, "lin_api_test")
	InitLinearPush()
	fireHook(id, "ENG-1", tasks.EventPlanDone)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "requirements.md") {
		t.Fatalf("stderr = %q, want requirements.md warning", msg)
	}
}

func TestLinearPush_AwaitApprovalEvent_PostsBoth(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "req", "plan")
	saveAPIKey(t, "lin_api_test")
	InitLinearPush()
	fireHook(id, "ENG-1", tasks.EventPlanAwaitApproval)
	got := env.recordedBodies()
	if len(got) != 3 {
		t.Fatalf("want 3 calls on await-approval, got %d: %v",
			len(got), got)
	}
}

func TestLinearPush_ReaperAwaitApprovalEvent_PostsBoth(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "req", "plan")
	saveAPIKey(t, "lin_api_test")
	InitLinearPush()
	fireHook(id, "ENG-1", tasks.EventReaperPlanAwaitApproval)
	got := env.recordedBodies()
	if len(got) != 3 {
		t.Fatalf("want 3 calls on reaper-await-approval, got %d",
			len(got))
	}
}

// TestLinearPush_NeedsClarificationDestination_NoHTTP pins the
// defensive guard: even if a future event lands inside the
// `isPlanSuccessEvent` set, the hook must short-circuit when
// `tr.To` is `needs-clarification` so it never tries to upload a
// plan.md that the planner did not write.
func TestLinearPush_NeedsClarificationDestination_NoHTTP(t *testing.T) {
	id := tasks.NewTaskID()
	env := newPushEnv(t, id, "req", "plan")
	saveAPIKey(t, "lin_api_test")
	InitLinearPush()
	fireHookTo(id, "ENG-1", tasks.EventPlanDone,
		tasks.StatusNeedsClarification)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %d: %v",
			len(got), got)
	}
}

func assertMutationVar(t *testing.T, body, key, want string) {
	t.Helper()
	var req struct {
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode body: %v (%s)", err, body)
	}
	if got := req.Variables[key]; got != want {
		t.Fatalf("variables[%q] = %v, want %q", key, got, want)
	}
}
