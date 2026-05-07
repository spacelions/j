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

	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// verifyPushEnv bundles the per-test scaffolding shared by the
// verify-push hook tests: an httptest-backed Linear endpoint that
// records every request body and a configurable response shape per
// query family, plus a redirected stderr pipe for warning assertions.
type verifyPushEnv struct {
	srv          *httptest.Server
	mu           sync.Mutex
	bodies       []string
	issueResp    *linear.Issue
	issueErrors  []string
	commentErrs  []string
	stderrR      *os.File
	stderrW      *os.File
	stderrOrig   *os.File
	stderrClosed bool
}

func newVerifyPushEnv(t *testing.T, taskID, findings string) *verifyPushEnv {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	dir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if findings != "" {
		writeFile(t,
			filepath.Join(dir, tasks.VerifierFindingsFileName),
			findings)
	}
	env := &verifyPushEnv{
		issueResp: &linear.Issue{
			ID: "node-1", Identifier: "ENG-1", Title: "t",
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

func (e *verifyPushEnv) stderrText(t *testing.T) string {
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

func (e *verifyPushEnv) recordedBodies() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.bodies))
	copy(out, e.bodies)
	return out
}

func (e *verifyPushEnv) handle(
	w http.ResponseWriter, r *http.Request,
) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	e.mu.Lock()
	e.bodies = append(e.bodies, string(body))
	e.mu.Unlock()
	q := string(body)
	switch {
	case strings.Contains(q, "commentCreate"):
		writeMutation(w, "commentCreate", e.commentErrs)
	case strings.Contains(q, "issue(id:"):
		writeIssueResp(w, e.issueResp, e.issueErrors)
	default:
		http.Error(w, "unknown query", http.StatusBadRequest)
	}
}

// fireVerifyHook dispatches a verifier transition through the global
// hook registry. Centralising the construction keeps individual cases
// focused on the assertion under test.
func fireVerifyHook(
	taskID, linearIssue string,
	to tasks.TaskStatus, ev tasks.Event,
) {
	tasks.Notify(
		tasks.Transition{
			From: tasks.StatusVerifying, Event: ev, To: to,
		},
		tasks.Task{
			ID: taskID, Status: to, LinearIssue: linearIssue,
		},
	)
}

// commentBody decodes the variables.body field from a recorded GraphQL
// request so test cases can assert on the rendered comment text.
func commentBody(t *testing.T, body string) string {
	t.Helper()
	var req struct {
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode body: %v (%s)", err, body)
	}
	got, _ := req.Variables["body"].(string)
	return got
}

// ============================== cases ==============================

func TestLinearVerifyPush_TerminalPass_PostsPlainComment(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushEnv(t, id, "findings body")
	saveAPIKey(t, "lin_api_test")
	InitLinearVerifyPush()
	fireVerifyHook(
		id, "ENG-1", tasks.StatusCompleted, tasks.EventVerifyPass)
	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf(
			"want 2 calls (issue, commentCreate), got %d: %v",
			len(got), got)
	}
	if !strings.Contains(got[1], "commentCreate") {
		t.Fatalf("call[1] not commentCreate: %s", got[1])
	}
	body := commentBody(t, got[1])
	if !strings.HasPrefix(body, "Verification passed") {
		t.Fatalf("body prefix: %q", body)
	}
	if strings.HasPrefix(body, "@") {
		t.Fatalf("body unexpectedly starts with mention: %q", body)
	}
	if !strings.Contains(body, "findings body") {
		t.Fatalf("body missing findings: %q", body)
	}
}

func TestLinearVerifyPush_TerminalFail_PostsPlainComment(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushEnv(t, id, "findings body")
	saveAPIKey(t, "lin_api_test")
	InitLinearVerifyPush()
	fireVerifyHook(
		id, "ENG-1", tasks.StatusFailed, tasks.EventVerifyFail)
	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(got), got)
	}
	body := commentBody(t, got[1])
	if !strings.HasPrefix(
		body, "Verification failed (retries exhausted)",
	) {
		t.Fatalf("body prefix: %q", body)
	}
}

func TestLinearVerifyPush_TerminalStuck_PostsPlainComment(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushEnv(t, id, "findings body")
	saveAPIKey(t, "lin_api_test")
	InitLinearVerifyPush()
	fireVerifyHook(
		id, "ENG-1", tasks.StatusFailed, tasks.EventVerifyStuck)
	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(got), got)
	}
	body := commentBody(t, got[1])
	if !strings.Contains(body, "Verification failed (retries exhausted)") {
		t.Fatalf("body: %q", body)
	}
}

func TestLinearVerifyPush_NonLinearTask_NoHTTP(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushEnv(t, id, "findings body")
	saveAPIKey(t, "lin_api_test")
	InitLinearVerifyPush()
	fireVerifyHook(id, "", tasks.StatusCompleted, tasks.EventVerifyPass)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %v", got)
	}
}

func TestLinearVerifyPush_NonTerminalEvent_NoHTTP(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushEnv(t, id, "findings body")
	saveAPIKey(t, "lin_api_test")
	InitLinearVerifyPush()
	fireVerifyHook(
		id, "ENG-1", tasks.StatusVerifying, tasks.EventVerifyResume)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %v", got)
	}
}

func TestLinearVerifyPush_MissingFindings_Warns(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushEnv(t, id, "")
	saveAPIKey(t, "lin_api_test")
	InitLinearVerifyPush()
	fireVerifyHook(
		id, "ENG-1", tasks.StatusCompleted, tasks.EventVerifyPass)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, tasks.VerifierFindingsFileName,
	) {
		t.Fatalf("stderr = %q, want findings filename warning", msg)
	}
}

func TestLinearVerifyPush_NoAPIKey_Warns(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushEnv(t, id, "findings body")
	InitLinearVerifyPush()
	fireVerifyHook(
		id, "ENG-1", tasks.StatusCompleted, tasks.EventVerifyPass)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "linear verify push",
	) {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearVerifyPush_LoadAPIKeyError_Warns(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushEnv(t, id, "findings body")
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
	InitLinearVerifyPush()
	fireVerifyHook(
		id, "ENG-1", tasks.StatusCompleted, tasks.EventVerifyPass)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(msg, "load api key") {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearVerifyPush_GetIssueFails_Warns(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushEnv(t, id, "findings body")
	env.issueResp = nil
	saveAPIKey(t, "lin_api_test")
	InitLinearVerifyPush()
	fireVerifyHook(
		id, "ENG-1", tasks.StatusCompleted, tasks.EventVerifyPass)
	got := env.recordedBodies()
	if len(got) != 1 || !strings.Contains(got[0], "issue(id:") {
		t.Fatalf("want one issue lookup only, got %v", got)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "linear verify push",
	) {
		t.Fatalf("stderr = %q", msg)
	}
}

func TestLinearVerifyPush_CommentFails_Warns(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushEnv(t, id, "findings body")
	env.commentErrs = []string{"nope"}
	saveAPIKey(t, "lin_api_test")
	InitLinearVerifyPush()
	fireVerifyHook(
		id, "ENG-1", tasks.StatusCompleted, tasks.EventVerifyPass)
	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(got), got)
	}
	if msg := env.stderrText(t); !strings.Contains(
		msg, "commentCreate",
	) {
		t.Fatalf("stderr = %q, want commentCreate warning", msg)
	}
}

// PushVerifyIterationFinding tests — exercised directly so the helper
// path used by the verifier loop is covered without relying on a full
// lifecycle Finish drive-through.

func TestPushVerifyIterationFinding_PostsHeaderedComment(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushEnv(t, id, "iter findings")
	saveAPIKey(t, "lin_api_test")
	task := tasks.Task{ID: id, LinearIssue: "ENG-1"}
	PushVerifyIterationFinding(io.Discard, task, 1, 3)
	got := env.recordedBodies()
	if len(got) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(got), got)
	}
	body := commentBody(t, got[1])
	if !strings.HasPrefix(
		body, "Verification iteration 2/3 failed",
	) {
		t.Fatalf("body prefix: %q", body)
	}
	if strings.HasPrefix(body, "@") {
		t.Fatalf("body unexpectedly starts with mention: %q", body)
	}
	if !strings.Contains(body, "iter findings") {
		t.Fatalf("body missing findings: %q", body)
	}
}

func TestPushVerifyIterationFinding_NoLinear_NoHTTP(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushEnv(t, id, "iter findings")
	saveAPIKey(t, "lin_api_test")
	task := tasks.Task{ID: id}
	PushVerifyIterationFinding(io.Discard, task, 0, 3)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %v", got)
	}
}

func TestPushVerifyIterationFinding_MissingFindings_Warns(t *testing.T) {
	id := tasks.NewTaskID()
	env := newVerifyPushEnv(t, id, "")
	saveAPIKey(t, "lin_api_test")
	task := tasks.Task{ID: id, LinearIssue: "ENG-1"}
	var stderr strings.Builder
	PushVerifyIterationFinding(&stderr, task, 0, 3)
	if got := env.recordedBodies(); len(got) != 0 {
		t.Fatalf("expected no HTTP traffic, got %v", got)
	}
	if !strings.Contains(stderr.String(), tasks.VerifierFindingsFileName) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
