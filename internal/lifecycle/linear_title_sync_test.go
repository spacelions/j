package lifecycle

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/tools/linear"
)

// titleSyncEnv mirrors stateSyncEnv but classifies issueUpdate
// requests by their bound input field so a title-update is
// distinguishable from a state-update in the same recorded stream.
// The hook only ever fires `issue` lookups and `issueUpdate(title)`
// mutations — anything else is a fast 400 to surface accidental
// reuse of state-sync routes.
type titleSyncEnv struct {
	srv          *httptest.Server
	mu           sync.Mutex
	bodies       []string
	issueResp    *linear.Issue
	issueErrors  []string
	updateErrors []string
	stderrR      *os.File
	stderrW      *os.File
	stderrOrig   *os.File
	stderrClosed bool
}

func newTitleSyncEnv(t *testing.T) *titleSyncEnv {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	env := &titleSyncEnv{
		issueResp: &linear.Issue{
			ID: "node-1", Identifier: "ENG-1", Title: "Plain title",
		},
	}
	env.srv = httptest.NewServer(http.HandlerFunc(env.handle))
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

func (e *titleSyncEnv) stderrText(t *testing.T) string {
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

func (e *titleSyncEnv) recordedBodies() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.bodies))
	copy(out, e.bodies)
	return out
}

func (e *titleSyncEnv) handle(
	w http.ResponseWriter, r *http.Request,
) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	e.mu.Lock()
	e.bodies = append(e.bodies, string(body))
	e.mu.Unlock()
	q := string(body)
	switch {
	case strings.Contains(q, "issueUpdate"):
		writeMutation(w, "issueUpdate", e.updateErrors)
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

func fireTitleSync(
	taskID, linearIssue string,
	from, to tasks.TaskStatus, ev tasks.Event,
) {
	tasks.Notify(
		tasks.Transition{From: from, Event: ev, To: to},
		tasks.Task{
			ID: taskID, Status: to, LinearIssue: linearIssue,
		},
	)
}

func TestDecorateTitle(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		status tasks.TaskStatus
		want   string
	}{
		{
			"normal+abnormal", "Build pipeline",
			tasks.StatusFailed, "❗ Build pipeline",
		},
		{
			"normal+needs-clarification", "Build pipeline",
			tasks.StatusNeedsClarification,
			"❗ Build pipeline",
		},
		{
			"normal+help", "Build pipeline",
			tasks.StatusHelp, "❗ Build pipeline",
		},
		{
			"normal+pending-approval", "Build pipeline",
			tasks.StatusPlanPendingApproval,
			"👀 Build pipeline",
		},
		{
			"normal+working", "Build pipeline",
			tasks.StatusWorking, "Build pipeline",
		},
		{
			"alert+same-status", "❗ Build pipeline",
			tasks.StatusFailed, "❗ Build pipeline",
		},
		{
			"alert+normal-strips", "❗ Build pipeline",
			tasks.StatusWorking, "Build pipeline",
		},
		{
			"alert-then-eyes", "❗ Build pipeline",
			tasks.StatusPlanPendingApproval,
			"👀 Build pipeline",
		},
		{
			"eyes-then-alert", "👀 Build pipeline",
			tasks.StatusFailed, "❗ Build pipeline",
		},
		{
			"doubled-alert+normal",
			"❗ ❗ Build pipeline",
			tasks.StatusWorking, "Build pipeline",
		},
		{
			"mixed+abnormal", "👀 ❗ Build pipeline",
			tasks.StatusFailed, "❗ Build pipeline",
		},
		{
			"alert-no-space+normal", "❗Build pipeline",
			tasks.StatusWorking, "Build pipeline",
		},
		{
			"eyes-no-space+normal", "👀Build pipeline",
			tasks.StatusWorking, "Build pipeline",
		},
		{
			"empty+normal", "",
			tasks.StatusWorking, "",
		},
		{
			"empty+abnormal", "",
			tasks.StatusFailed, "❗ ",
		},
		{
			"completed-strips-eyes", "👀 Build pipeline",
			tasks.StatusCompleted, "Build pipeline",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decorateTitle(tc.in, tc.status)
			if got != tc.want {
				t.Fatalf("decorateTitle(%q,%q) = %q, want %q",
					tc.in, tc.status, got, tc.want)
			}
		})
	}
}

func TestLinearTitleSync_NoLinearIssue_NoHTTP(t *testing.T) {
	env := newTitleSyncEnv(t)
	saveAPIKey(t, "lin_api_test")
	InitLinearTitleSync()
	fireTitleSync("task-1", "",
		tasks.StatusPlanning, tasks.StatusFailed,
		tasks.EventVerifyStuck)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no traffic, got %v", got)
	}
}

func TestLinearTitleSync_NoAPIKey_Warns(t *testing.T) {
	env := newTitleSyncEnv(t)
	InitLinearTitleSync()
	fireTitleSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusFailed,
		tasks.EventVerifyStuck)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no traffic, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(msg, "linear sync") {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearTitleSync_GetIssueFails_Warns(t *testing.T) {
	env := newTitleSyncEnv(t)
	env.issueResp = nil
	saveAPIKey(t, "lin_api_test")
	InitLinearTitleSync()
	fireTitleSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusFailed,
		tasks.EventVerifyStuck)
	got := env.recordedBodies()
	if len(got) != 1 || !strings.Contains(got[0], "issue(id:") {
		t.Fatalf("expected one issue lookup, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(msg, "resolve") {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearTitleSync_TitleAlreadyCorrect_NoUpdate(t *testing.T) {
	env := newTitleSyncEnv(t)
	env.issueResp = &linear.Issue{
		ID: "node-1", Identifier: "ENG-1",
		Title: "❗ Already decorated",
	}
	saveAPIKey(t, "lin_api_test")
	InitLinearTitleSync()
	fireTitleSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusFailed,
		tasks.EventVerifyStuck)
	got := env.recordedBodies()
	if len(got) != 1 || !strings.Contains(got[0], "issue(id:") {
		t.Fatalf("expected only the lookup, got %v", got)
	}
}

func TestLinearTitleSync_NeedsUpdate_SendsTitle(t *testing.T) {
	env := newTitleSyncEnv(t)
	env.issueResp = &linear.Issue{
		ID: "node-1", Identifier: "ENG-1", Title: "Plain title",
	}
	saveAPIKey(t, "lin_api_test")
	InitLinearTitleSync()
	fireTitleSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusFailed,
		tasks.EventVerifyStuck)
	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[1], "issueUpdate") ||
		!strings.Contains(got[1], "title:$title") {
		t.Fatalf("expected title issueUpdate, got %s", got[1])
	}
	assertVarStr(t, got[1], "id", "node-1")
	assertVarStr(t, got[1], "title", "❗ Plain title")
}

func TestLinearTitleSync_StripsPrefix_OnNormalStatus(t *testing.T) {
	env := newTitleSyncEnv(t)
	env.issueResp = &linear.Issue{
		ID: "node-1", Identifier: "ENG-1",
		Title: "❗ Plain title",
	}
	saveAPIKey(t, "lin_api_test")
	InitLinearTitleSync()
	fireTitleSync("task-1", "ENG-1",
		tasks.StatusFailed, tasks.StatusWorking,
		tasks.EventWorkResume)
	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(got), got)
	}
	assertVarStr(t, got[1], "title", "Plain title")
}

func TestLinearTitleSync_PendingApproval_AddsEyes(t *testing.T) {
	env := newTitleSyncEnv(t)
	env.issueResp = &linear.Issue{
		ID: "node-1", Identifier: "ENG-1", Title: "Plain title",
	}
	saveAPIKey(t, "lin_api_test")
	InitLinearTitleSync()
	fireTitleSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusPlanPendingApproval,
		tasks.EventPlanAwaitApproval)
	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(got), got)
	}
	assertVarStr(t, got[1], "title", "👀 Plain title")
}

func TestLinearTitleSync_UpdateFails_Warns(t *testing.T) {
	env := newTitleSyncEnv(t)
	env.updateErrors = []string{"boom"}
	saveAPIKey(t, "lin_api_test")
	InitLinearTitleSync()
	fireTitleSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusFailed,
		tasks.EventVerifyStuck)
	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(got), got)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "issueUpdate title") {
		t.Fatalf("stderr = %q, want title-update warning", msg)
	}
}

func TestLinearTitleSync_LoadAPIKeyError_Warns(t *testing.T) {
	env := newTitleSyncEnv(t)
	path := store.DefaultPath()
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	InitLinearTitleSync()
	fireTitleSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusFailed,
		tasks.EventVerifyStuck)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no traffic, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "load api key") {
		t.Fatalf("stderr = %q", msg)
	}
}

// TestLinearTitleSync_HandlesJSONEscape verifies that titles
// containing characters needing JSON escaping survive the round-trip
// — the variable shape uses a string type so no manual escaping is
// needed, but a regression here would silently drop user content.
func TestLinearTitleSync_HandlesJSONEscape(t *testing.T) {
	env := newTitleSyncEnv(t)
	env.issueResp = &linear.Issue{
		ID: "node-1", Identifier: "ENG-1",
		Title: `Title "with" quotes`,
	}
	saveAPIKey(t, "lin_api_test")
	InitLinearTitleSync()
	fireTitleSync("task-1", "ENG-1",
		tasks.StatusPlanning, tasks.StatusFailed,
		tasks.EventVerifyStuck)
	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(got), got)
	}
	var req struct {
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal([]byte(got[1]), &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	want := `❗ Title "with" quotes`
	if req.Variables["title"] != want {
		t.Fatalf("title = %v, want %q", req.Variables["title"], want)
	}
}
