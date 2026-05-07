package testcases_test

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

// linearStateSyncEnv is the verifier-side scaffolding for the
// linear-state-sync acceptance tests: a stub Linear endpoint that
// records every request body, plus a redirected stderr pipe so
// hook warnings round-trip.
type linearStateSyncEnv struct {
	srv          *httptest.Server
	mu           sync.Mutex
	bodies       []string
	issueResp    *linearIssueStub
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

func newLinearStateSyncEnv(t *testing.T) *linearStateSyncEnv {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	env := &linearStateSyncEnv{
		issueResp: &linearIssueStub{
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

func (e *linearStateSyncEnv) handle(
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
		writeStatesAck(w, e.states, e.statesErrors)
	case strings.Contains(q, "issueUpdate"):
		writeMutationResp(w, "issueUpdate", e.updateErrors)
	case strings.Contains(q, "commentCreate"):
		writeMutationResp(w, "commentCreate", e.commentErrs)
	case strings.Contains(q, "viewer{id"):
		writeViewerAck(w, e.viewerID, e.viewerErrors)
	case strings.Contains(q, "issue(id:"):
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"issue": e.issueResp},
		})
	default:
		http.Error(w, "unknown query", http.StatusBadRequest)
	}
}

func writeStatesAck(
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
		out := make([]map[string]string, 0, len(errs))
		for _, m := range errs {
			out = append(out, map[string]string{"message": m})
		}
		payload["errors"] = out
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func writeViewerAck(
	w http.ResponseWriter, id string, errs []string,
) {
	payload := map[string]any{
		"data": map[string]any{
			"viewer": map[string]any{"id": id},
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

func (e *linearStateSyncEnv) recordedBodies() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.bodies))
	copy(out, e.bodies)
	return out
}

func (e *linearStateSyncEnv) stderrText(t *testing.T) string {
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

func fireStateSyncTransition(
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

func bodyKindList(bodies []string) []string {
	out := make([]string, 0, len(bodies))
	for _, b := range bodies {
		switch {
		case strings.Contains(b, "team{states"):
			out = append(out, "states")
		case strings.Contains(b, "issueUpdate"):
			out = append(out, "issueUpdate")
		case strings.Contains(b, "commentCreate"):
			out = append(out, "commentCreate")
		case strings.Contains(b, "viewer{id"):
			out = append(out, "viewer")
		case strings.Contains(b, "issue(id:"):
			out = append(out, "issue")
		default:
			out = append(out, "?")
		}
	}
	return out
}

func equalSlices(got, want []string) bool {
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
