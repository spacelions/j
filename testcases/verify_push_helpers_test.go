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

// verifyPushAcceptanceEnv is the verifier-side scaffolding for the
// linear-verify-push acceptance tests: a stub Linear endpoint that
// records every request body, plus a redirected stderr pipe so the
// hook's warnings round-trip back into the assertions. Mirrors the
// linear_state_sync_helpers_test.go shape so the verify-push tests
// share the same vocabulary as the state-sync tests.
type verifyPushAcceptanceEnv struct {
	srv          *httptest.Server
	mu           sync.Mutex
	bodies       []string
	issueResp    *linearIssueStub
	commentErrs  []string
	stderrR      *os.File
	stderrW      *os.File
	stderrOrig   *os.File
	stderrClosed bool
}

func newVerifyPushAcceptanceEnv(
	t *testing.T, taskID, findings string,
) *verifyPushAcceptanceEnv {
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
		writeArtefact(t,
			filepath.Join(dir, tasks.VerifierFindingsFileName),
			findings)
	}
	env := &verifyPushAcceptanceEnv{
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

func (e *verifyPushAcceptanceEnv) handle(
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
		writeMutationResp(w, "commentCreate", e.commentErrs)
	case strings.Contains(q, "issue(id:"):
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"issue": e.issueResp},
		})
	default:
		http.Error(w, "unknown query", http.StatusBadRequest)
	}
}

func (e *verifyPushAcceptanceEnv) recordedBodies() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.bodies))
	copy(out, e.bodies)
	return out
}

func (e *verifyPushAcceptanceEnv) stderrText(t *testing.T) string {
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

// fireVerifyTerminal dispatches a terminal verifying-> transition
// through the global hook registry so verify-push acceptance tests
// can observe the InitLinearVerifyPush hook reaction without driving
// the full FSM through ApplyAndPersist.
func fireVerifyTerminal(
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
