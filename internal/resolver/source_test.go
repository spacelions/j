package resolver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/tools/linear"
)

type sourceUI struct {
	source          picker.Source
	md              string
	taskID          string
	ok              bool
	err             error
	linearAPIKey    string
	linearAPIKeyOK  bool
	pickedProject   linear.Project
	pickedProjectOK bool
	pickedIssue     linear.Issue
	pickedIssueOK   bool
}

func (u sourceUI) SelectSource(context.Context, []picker.Source) (picker.Source, error) {
	return u.source, u.err
}

func (u sourceUI) PickMarkdownInCwd(context.Context) (string, error) {
	return u.md, u.err
}

func (u sourceUI) PickTask(context.Context, string, []tasks.Task) (string, bool, error) {
	return u.taskID, u.ok, u.err
}

func (u sourceUI) PromptLinearAPIKey(context.Context, string) (string, bool, error) {
	return u.linearAPIKey, u.linearAPIKeyOK, u.err
}

func (u sourceUI) PickLinearProject(context.Context, []linear.Project) (linear.Project, bool, error) {
	return u.pickedProject, u.pickedProjectOK, u.err
}

func (u sourceUI) PickLinearIssue(context.Context, []linear.Issue) (linear.Issue, bool, error) {
	return u.pickedIssue, u.pickedIssueOK, u.err
}

func TestResolveStartTargetFromFile(t *testing.T) {
	setupResolverProject(t)
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte("# task"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := ResolveStartTarget(t.Context(), sourceUI{}, bytes.NewBuffer(nil), path)
	if err != nil {
		t.Fatalf("ResolveStartTarget: %v", err)
	}
	if !target.IsNew || target.Body != "# task" || target.Source != path {
		t.Fatalf("target = %+v", target)
	}
}

func TestResolveStartTargetSources(t *testing.T) {
	setupResolverProject(t)
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := ResolveStartTarget(t.Context(), sourceUI{source: picker.SourceMarkdown, md: path}, bytes.NewBuffer(nil), "")
	if err != nil || !target.IsNew || target.Body != "body" {
		t.Fatalf("markdown target = %+v, %v", target, err)
	}

	seedResolverTask(t, tasks.Task{ID: "existing", Status: tasks.StatusPlanDone}, "plan", "# req\nbody")
	target, err = ResolveStartTarget(t.Context(), sourceUI{source: picker.SourceTask, taskID: "existing", ok: true}, bytes.NewBuffer(nil), "")
	if err != nil || target.TaskID != "existing" || target.IsNew {
		t.Fatalf("task target = %+v, %v", target, err)
	}

	// Linear source: stub the GraphQL endpoint at a httptest.Server
	// via linear.TestEndpoint and pre-save an API key so the picker
	// skips the link prompt and StartTargetFromLinear actually
	// fetches the issue.
	if err := linear.SaveAPIKey("lin_api_test"); err != nil {
		t.Fatal(err)
	}
	if err := linear.SaveProject("project-x"); err != nil {
		t.Fatal(err)
	}
	srv := newLinearStubServer(stubLinearResponses{
		issueByID: map[string]stubLinearIssue{
			"ENG-1": {Identifier: "ENG-1", Title: "from picker", Description: "body", URL: "https://linear.app/eng/issue/ENG-1"},
		},
		assignedIssues: []stubLinearIssue{
			{Identifier: "ENG-1", Title: "from picker", URL: "https://linear.app/eng/issue/ENG-1", State: "In Progress"},
		},
	})
	t.Cleanup(srv.Close)
	prev := linear.TestEndpoint
	linear.TestEndpoint = srv.URL
	t.Cleanup(func() { linear.TestEndpoint = prev })

	target, err = ResolveStartTarget(
		t.Context(),
		sourceUI{
			source:        picker.SourceLinear,
			pickedIssue:   linear.Issue{Identifier: "ENG-1", Title: "from picker", State: "In Progress"},
			pickedIssueOK: true,
		},
		bytes.NewBuffer(nil),
		"",
	)
	if err != nil {
		t.Fatalf("linear target err = %v", err)
	}
	if !target.IsNew || !strings.Contains(target.Body, "from picker") || target.Source != "linear:ENG-1" {
		t.Fatalf("linear target = %+v", target)
	}
}

// TestFetchLinearBody_Errors covers the validation, missing-key,
// and not-found branches in FetchLinearBody so the linear: prefix
// errors surface unchanged.
func TestFetchLinearBody_Errors(t *testing.T) {
	setupResolverProject(t)

	// Invalid identifier: short-circuit before LoadAPIKey runs.
	if _, _, err := FetchLinearBody(t.Context(), "foo"); !errors.Is(err, linear.ErrInvalidIdentifier) {
		t.Fatalf("invalid id err = %v", err)
	}

	// Valid identifier but no token: ErrNoAPIKey.
	if _, _, err := FetchLinearBody(t.Context(), "ENG-1"); !errors.Is(err, linear.ErrNoAPIKey) {
		t.Fatalf("no key err = %v", err)
	}

	// Valid identifier + token + 401 server: ErrUnauthorized.
	if err := linear.SaveAPIKey("lin_api_test"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	prev := linear.TestEndpoint
	linear.TestEndpoint = srv.URL
	t.Cleanup(func() { linear.TestEndpoint = prev })
	if _, _, err := FetchLinearBody(t.Context(), "ENG-1"); !errors.Is(err, linear.ErrUnauthorized) {
		t.Fatalf("401 err = %v", err)
	}
}

// TestStartTargetFromLinear_Success drives the happy path: with a
// stored token + stub server, the helper returns a StartTarget
// whose body carries the issue's title + footer and whose Source
// is the linear: identifier label.
func TestStartTargetFromLinear_Success(t *testing.T) {
	setupResolverProject(t)
	if err := linear.SaveAPIKey("lin_api_test"); err != nil {
		t.Fatal(err)
	}
	srv := newLinearStubServer(stubLinearResponses{
		issueByID: map[string]stubLinearIssue{
			"ENG-2": {Identifier: "ENG-2", Title: "the title", Description: "the description", URL: "https://linear.app/eng/issue/ENG-2"},
		},
	})
	t.Cleanup(srv.Close)
	prev := linear.TestEndpoint
	linear.TestEndpoint = srv.URL
	t.Cleanup(func() { linear.TestEndpoint = prev })

	target, err := StartTargetFromLinear(t.Context(), "ENG-2")
	if err != nil {
		t.Fatalf("StartTargetFromLinear: %v", err)
	}
	if !target.IsNew || target.Source != "linear:ENG-2" {
		t.Fatalf("target = %+v", target)
	}
	if !strings.HasPrefix(target.Body, "# the title") {
		t.Fatalf("body should start with the issue title; got %q", target.Body)
	}
	if !strings.Contains(target.Body, "Linear: https://linear.app/eng/issue/ENG-2") {
		t.Fatalf("body should carry the Linear URL footer; got %q", target.Body)
	}
}

// TestStartTargetFromLinear_PropagatesError pins the error pass-through:
// no API key stored, helper surfaces ErrNoAPIKey verbatim.
func TestStartTargetFromLinear_PropagatesError(t *testing.T) {
	setupResolverProject(t)
	_, err := StartTargetFromLinear(t.Context(), "ENG-1")
	if !errors.Is(err, linear.ErrNoAPIKey) {
		t.Fatalf("err = %v, want ErrNoAPIKey", err)
	}
}

type stubLinearIssue struct {
	Identifier  string
	Title       string
	Description string
	URL         string
	State       string
}

type stubLinearResponses struct {
	issueByID      map[string]stubLinearIssue
	assignedIssues []stubLinearIssue
}

// newLinearStubServer returns a test GraphQL endpoint that
// dispatches by query content: viewer.assignedIssues queries
// receive responses.assignedIssues, and issue(id:...) queries get
// the matching entry from responses.issueByID. Anything else is a
// 400 so a typo in production code is loud.
func newLinearStubServer(responses stubLinearResponses) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(req.Query, "viewer{assignedIssues") {
			nodes := make([]map[string]any, 0, len(responses.assignedIssues))
			for _, iss := range responses.assignedIssues {
				nodes = append(nodes, map[string]any{
					"identifier": iss.Identifier,
					"title":      iss.Title,
					"url":        iss.URL,
					"state":      map[string]string{"name": iss.State},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"viewer": map[string]any{
						"assignedIssues": map[string]any{"nodes": nodes},
					},
				},
			})
			return
		}
		if strings.Contains(req.Query, "issue(id:$id)") {
			id, _ := req.Variables["id"].(string)
			iss, ok := responses.issueByID[id]
			if !ok {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"issue": nil}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"issue": map[string]string{
						"identifier":  iss.Identifier,
						"title":       iss.Title,
						"description": iss.Description,
						"url":         iss.URL,
					},
				},
			})
			return
		}
		http.Error(w, "stub: unknown query", http.StatusBadRequest)
	}))
}

func TestResolveStartTargetErrorsAndCancel(t *testing.T) {
	setupResolverProject(t)
	_, err := ResolveStartTarget(t.Context(), sourceUI{err: errors.New("select failed")}, bytes.NewBuffer(nil), "")
	if err == nil || !strings.Contains(err.Error(), "select failed") {
		t.Fatalf("select err = %v", err)
	}

	seedResolverTask(t, tasks.Task{ID: "existing", Status: tasks.StatusPlanDone}, "plan", "")
	target, err := ResolveStartTarget(t.Context(), sourceUI{source: picker.SourceTask, ok: false}, bytes.NewBuffer(nil), "")
	if err != nil || target.TaskID != "" {
		t.Fatalf("cancel target = %+v, %v", target, err)
	}

	_, err = ResolveStartTarget(t.Context(), sourceUI{source: picker.Source("bad")}, bytes.NewBuffer(nil), "")
	if err == nil || !strings.Contains(err.Error(), "unsupported source") {
		t.Fatalf("bad source err = %v", err)
	}
}
