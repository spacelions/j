package testcases_test

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

// linearPushEnv is the verifier-side scaffolding for the
// linear-push acceptance tests: a stub Linear endpoint that records
// every request body, a redirected stderr pipe, plus configurable
// failure injection on each mutation.
type linearPushEnv struct {
	srv          *httptest.Server
	mu           sync.Mutex
	bodies       []string
	issueResp    *linearIssueStub
	updateErrors []string
	commentErrs  []string
	stderrR      *os.File
	stderrW      *os.File
	stderrOrig   *os.File
	stderrClosed bool
}

type linearIssueStub struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

func newLinearPushEnv(
	t *testing.T, taskID, req, plan string,
) *linearPushEnv {
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
		writeArtefact(t,
			filepath.Join(dir, tasks.RequirementsFileName), req)
	}
	if plan != "" {
		writeArtefact(t, filepath.Join(dir, tasks.PlanFileName), plan)
	}
	env := &linearPushEnv{
		issueResp: &linearIssueStub{
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

func (e *linearPushEnv) handle(
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
		writeMutationResp(w, "issueUpdate", e.updateErrors)
	case strings.Contains(q, "commentCreate"):
		writeMutationResp(w, "commentCreate", e.commentErrs)
	case strings.Contains(q, "issue(id:"):
		payload := map[string]any{
			"data": map[string]any{"issue": e.issueResp},
		}
		_ = json.NewEncoder(w).Encode(payload)
	default:
		http.Error(w, "unknown query", http.StatusBadRequest)
	}
}

func (e *linearPushEnv) recordedBodies() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.bodies))
	copy(out, e.bodies)
	return out
}

func (e *linearPushEnv) stderrText(t *testing.T) string {
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

func writeMutationResp(
	w http.ResponseWriter, field string, errs []string,
) {
	payload := map[string]any{
		"data": map[string]any{
			field: map[string]any{"success": len(errs) == 0},
		},
	}
	if len(errs) > 0 {
		out := make([]map[string]string, 0, len(errs))
		for _, m := range errs {
			out = append(out, map[string]string{"message": m})
		}
		payload["errors"] = out
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func writeArtefact(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func saveLinearAPIKey(t *testing.T, token string) {
	t.Helper()
	if err := linear.SaveAPIKey(token); err != nil {
		t.Fatalf("SaveAPIKey: %v", err)
	}
}

func firePlanDone(taskID, linearIssue string, ev tasks.Event) {
	tasks.Notify(
		tasks.Transition{
			From:  tasks.StatusPlanning,
			Event: ev,
			To:    tasks.StatusPlanDone,
		},
		tasks.Task{
			ID: taskID, Status: tasks.StatusPlanDone,
			LinearIssue: linearIssue,
		},
	)
}

func decodeMutationVar(
	t *testing.T, body, key string,
) string {
	t.Helper()
	var req struct {
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode body: %v (%s)", err, body)
	}
	v, _ := req.Variables[key].(string)
	return v
}
