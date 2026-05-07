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

	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// stateSyncEnv bundles the per-test scaffolding the state-sync hook
// tests reuse: a stub Linear endpoint that records every request,
// configurable failure injection per query family, and a redirected
// stderr pipe for warning assertions.
type stateSyncEnv struct {
	srv          *httptest.Server
	mu           sync.Mutex
	bodies       []string
	issueResp    *linear.Issue
	issueErrors  []string
	states       []linear.WorkflowState
	statesErrors []string
	updateErrors []string
	viewerID     string
	viewerErrors []string
	commentErrs  []string
	stderrR      *os.File
	stderrW      *os.File
	stderrOrig   *os.File
	stderrClosed bool
}

func newStateSyncEnv(t *testing.T) *stateSyncEnv {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	env := &stateSyncEnv{
		issueResp: &linear.Issue{
			ID: "node-1", Identifier: "ENG-1", Title: "t",
		},
		states: []linear.WorkflowState{
			{ID: "s-todo", Name: "Todo", Type: "unstarted"},
			{ID: "s-prog", Name: "In Progress", Type: "started"},
			{ID: "s-rev", Name: "In Review", Type: "started"},
		},
		viewerID: "user-uuid",
	}
	env.srv = httptest.NewServer(http.HandlerFunc(env.handle))
	t.Cleanup(env.srv.Close)
	prev := linear.TestEndpoint
	linear.TestEndpoint = env.srv.URL
	t.Cleanup(func() { linear.TestEndpoint = prev })
	tasks.ResetHooksForTest()
	t.Cleanup(tasks.ResetHooksForTest)
	installStderrPipe(t, env)
	return env
}

func installStderrPipe(t *testing.T, env *stateSyncEnv) {
	t.Helper()
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
}

func (e *stateSyncEnv) stderrText(t *testing.T) string {
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

func (e *stateSyncEnv) recordedBodies() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.bodies))
	copy(out, e.bodies)
	return out
}

func (e *stateSyncEnv) handle(
	w http.ResponseWriter, r *http.Request,
) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	e.mu.Lock()
	e.bodies = append(e.bodies, string(body))
	e.mu.Unlock()
	q := string(body)
	switch {
	case strings.Contains(q, "team{states"):
		writeStatesResp(w, e.states, e.statesErrors)
	case strings.Contains(q, "issueUpdate"):
		writeMutation(w, "issueUpdate", e.updateErrors)
	case strings.Contains(q, "commentCreate"):
		writeMutation(w, "commentCreate", e.commentErrs)
	case strings.Contains(q, "viewer{id"):
		writeViewerResp(w, e.viewerID, e.viewerErrors)
	case strings.Contains(q, "issue(id:"):
		writeIssueResp(w, e.issueResp, e.issueErrors)
	default:
		http.Error(w, "unknown query", http.StatusBadRequest)
	}
}

func writeIssueResp(
	w http.ResponseWriter, issue *linear.Issue, errs []string,
) {
	payload := map[string]any{
		"data": map[string]any{"issue": issue},
	}
	if len(errs) > 0 {
		payload["errors"] = errorList(errs)
	}
	writeJSON(w, payload)
}

func writeStatesResp(
	w http.ResponseWriter,
	states []linear.WorkflowState, errs []string,
) {
	payload := map[string]any{
		"data": map[string]any{
			"issue": map[string]any{
				"team": map[string]any{
					"states": map[string]any{"nodes": states},
				},
			},
		},
	}
	if len(errs) > 0 {
		payload["errors"] = errorList(errs)
	}
	writeJSON(w, payload)
}

func writeViewerResp(
	w http.ResponseWriter, id string, errs []string,
) {
	payload := map[string]any{
		"data": map[string]any{
			"viewer": map[string]any{"id": id},
		},
	}
	if len(errs) > 0 {
		payload["errors"] = errorList(errs)
	}
	writeJSON(w, payload)
}

// fireStateSync dispatches a synthetic transition to registered
// hooks. Centralising the construction keeps each case focused on
// the behaviour under test rather than the lifecycle plumbing.
func fireStateSync(
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

func bodyKinds(bodies []string) []string {
	kinds := make([]string, 0, len(bodies))
	for _, b := range bodies {
		kinds = append(kinds, classifyBody(b))
	}
	return kinds
}

func classifyBody(body string) string {
	switch {
	case strings.Contains(body, "team{states"):
		return "states"
	case strings.Contains(body, "issueUpdate"):
		return "issueUpdate"
	case strings.Contains(body, "commentCreate"):
		return "commentCreate"
	case strings.Contains(body, "viewer{id"):
		return "viewer"
	case strings.Contains(body, "issue(id:"):
		return "issue"
	}
	return "?"
}

func equalKinds(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func assertVarStr(t *testing.T, body, key, want string) {
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
