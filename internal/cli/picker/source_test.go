package picker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// scriptedSourceUI is a SourceUI fake that pre-canned answers.
type scriptedSourceUI struct {
	source      Source
	sourceErr   error
	markdown    string
	markdownErr error
	taskID      string
	taskOK      bool
	taskErr     error
	taskTitle   string
	mdCalls     int
	taskCalls   int
	sourceCalls int
	allowedSeen []Source

	linearAPIKey       string
	linearAPIKeyOK     bool
	linearAPIKeyErr    error
	linearAPIKeyURL    string
	linearAPIKeyCalls  int
	pickedProject      linear.Project
	pickedProjectOK    bool
	pickedProjectErr   error
	pickedProjectCalls int
	pickedProjectsSeen []linear.Project
	pickedIssue        linear.Issue
	pickedIssueOK      bool
	pickedIssueErr     error
	pickedIssueCalls   int
	pickedIssuesSeen   []linear.Issue
}

func (s *scriptedSourceUI) SelectSource(_ context.Context, allowed []Source) (Source, error) {
	s.sourceCalls++
	s.allowedSeen = append([]Source(nil), allowed...)
	if s.sourceErr != nil {
		return "", s.sourceErr
	}
	return s.source, nil
}

func (s *scriptedSourceUI) PickMarkdownInCwd(_ context.Context) (string, error) {
	s.mdCalls++
	return s.markdown, s.markdownErr
}

func (s *scriptedSourceUI) PickTask(_ context.Context, title string, _ []tasks.Task) (string, bool, error) {
	s.taskCalls++
	s.taskTitle = title
	if s.taskErr != nil {
		return "", false, s.taskErr
	}
	return s.taskID, s.taskOK, nil
}

func (s *scriptedSourceUI) PromptLinearAPIKey(_ context.Context, openURL string) (string, bool, error) {
	s.linearAPIKeyCalls++
	s.linearAPIKeyURL = openURL
	if s.linearAPIKeyErr != nil {
		return "", false, s.linearAPIKeyErr
	}
	return s.linearAPIKey, s.linearAPIKeyOK, nil
}

func (s *scriptedSourceUI) PickLinearProject(_ context.Context, projects []linear.Project) (linear.Project, bool, error) {
	s.pickedProjectCalls++
	s.pickedProjectsSeen = append([]linear.Project(nil), projects...)
	if s.pickedProjectErr != nil {
		return linear.Project{}, false, s.pickedProjectErr
	}
	return s.pickedProject, s.pickedProjectOK, nil
}

func (s *scriptedSourceUI) PickLinearIssue(_ context.Context, issues []linear.Issue) (linear.Issue, bool, error) {
	s.pickedIssueCalls++
	s.pickedIssuesSeen = append([]linear.Issue(nil), issues...)
	if s.pickedIssueErr != nil {
		return linear.Issue{}, false, s.pickedIssueErr
	}
	return s.pickedIssue, s.pickedIssueOK, nil
}

// stubAssignedIssuesServer points linear.TestEndpoint at an
// httptest.Server that returns the supplied issues for any
// viewer.assignedIssues query and an empty payload for everything
// else. The endpoint override is reset on t.Cleanup.
func stubAssignedIssuesServer(t *testing.T, issues ...linear.Issue) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nodes := make([]map[string]any, 0, len(issues))
		for _, iss := range issues {
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
	}))
	t.Cleanup(srv.Close)
	prev := linear.TestEndpoint
	linear.TestEndpoint = srv.URL
	t.Cleanup(func() { linear.TestEndpoint = prev })
}

func TestPickSource_Markdown(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceMarkdown, markdown: "/abs/feature.md"}
	res, err := PickSource(t.Context(), ui, []Source{SourceMarkdown, SourceLinear, SourceTask}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Source != SourceMarkdown || res.Markdown != "/abs/feature.md" || res.TaskID != "" || res.Cancelled {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickSource_Linear_TokenAndProjectStored(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	if err := linear.SaveAPIKey("lin_api_test"); err != nil {
		t.Fatal(err)
	}
	if err := linear.SaveProject("project-1"); err != nil {
		t.Fatal(err)
	}
	stubAssignedIssuesServer(t,
		linear.Issue{Identifier: "ENG-12", Title: "do the thing", State: "In Progress", URL: "https://linear.app/eng/issue/ENG-12"},
	)
	ui := &scriptedSourceUI{
		source: SourceLinear,
		pickedIssue: linear.Issue{
			Identifier: "ENG-12",
			Title:      "do the thing",
			State:      "In Progress",
			URL:        "https://linear.app/eng/issue/ENG-12",
		},
		pickedIssueOK: true,
	}
	res, err := PickSource(t.Context(), ui, []Source{SourceMarkdown, SourceLinear}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Source != SourceLinear || res.LinearIdentifier != "ENG-12" || res.Cancelled {
		t.Fatalf("res = %+v", res)
	}
	if ui.linearAPIKeyCalls != 0 {
		t.Fatalf("PromptLinearAPIKey should not fire when token is stored: calls=%d", ui.linearAPIKeyCalls)
	}
	if ui.pickedProjectCalls != 0 {
		t.Fatalf("PickLinearProject should not fire when project is stored: calls=%d", ui.pickedProjectCalls)
	}
	if ui.pickedIssueCalls != 1 {
		t.Fatalf("PickLinearIssue should fire once: calls=%d", ui.pickedIssueCalls)
	}
	if len(ui.pickedIssuesSeen) != 1 || ui.pickedIssuesSeen[0].Identifier != "ENG-12" {
		t.Fatalf("PickLinearIssue saw %+v, want one issue (ENG-12)", ui.pickedIssuesSeen)
	}
}

func TestPickSource_Linear_IssuePickerCancelled(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	if err := linear.SaveAPIKey("lin_api_test"); err != nil {
		t.Fatal(err)
	}
	if err := linear.SaveProject("p"); err != nil {
		t.Fatal(err)
	}
	stubAssignedIssuesServer(t,
		linear.Issue{Identifier: "ENG-1", Title: "x", State: "Todo"},
	)
	ui := &scriptedSourceUI{source: SourceLinear, pickedIssueOK: false}
	res, err := PickSource(t.Context(), ui, []Source{SourceLinear}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Source != SourceLinear || !res.Cancelled || res.LinearIdentifier != "" {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickSource_Linear_EmptyIssueList(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	if err := linear.SaveAPIKey("lin_api_test"); err != nil {
		t.Fatal(err)
	}
	if err := linear.SaveProject("p"); err != nil {
		t.Fatal(err)
	}
	stubAssignedIssuesServer(t)
	ui := &scriptedSourceUI{source: SourceLinear}
	_, err := PickSource(t.Context(), ui, []Source{SourceLinear}, nil, nil)
	if err == nil || err.Error() != "no Linear issues assigned to you" {
		t.Fatalf("err = %v, want empty-list error", err)
	}
	if ui.pickedIssueCalls != 0 {
		t.Fatalf("PickLinearIssue should not fire on empty list: calls=%d", ui.pickedIssueCalls)
	}
}

func TestPickSource_Linear_TokenPromptCancelled(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	ui := &scriptedSourceUI{source: SourceLinear, linearAPIKeyOK: false}
	res, err := PickSource(t.Context(), ui, []Source{SourceLinear}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !res.Cancelled || res.Source != SourceLinear {
		t.Fatalf("res = %+v", res)
	}
	if ui.linearAPIKeyCalls != 1 {
		t.Fatalf("PromptLinearAPIKey calls = %d, want 1", ui.linearAPIKeyCalls)
	}
	got, _ := linear.LoadAPIKey()
	if got != "" {
		t.Fatalf("token should not be saved on cancel: got %q", got)
	}
}

func TestPickSource_Linear_TokenPromptError(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	want := errors.New("token boom")
	ui := &scriptedSourceUI{source: SourceLinear, linearAPIKeyErr: want}
	_, err := PickSource(t.Context(), ui, []Source{SourceLinear}, nil, nil)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrapped 'token boom'", err)
	}
}

func TestPickSource_Task_HappyPath(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceTask, taskID: "01ABC", taskOK: true}
	listTasks := func() ([]tasks.Task, error) {
		return []tasks.Task{{ID: "01ABC", Status: tasks.StatusPlanDone, Summary: "x"}}, nil
	}
	res, err := PickSource(t.Context(), ui, []Source{SourceTask}, listTasks, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Source != SourceTask || res.TaskID != "01ABC" || res.Cancelled {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(ui.taskTitle, "existing task") {
		t.Fatalf("taskTitle = %q, want to mention existing task", ui.taskTitle)
	}
}

func TestPickSource_Task_UserCancelled(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceTask, taskOK: false}
	listTasks := func() ([]tasks.Task, error) {
		return []tasks.Task{{ID: "01ABC"}}, nil
	}
	res, err := PickSource(t.Context(), ui, []Source{SourceTask}, listTasks, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Source != SourceTask || !res.Cancelled || res.TaskID != "" {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickSource_Task_NilListTasks(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceTask}
	_, err := PickSource(t.Context(), ui, []Source{SourceTask}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "listTasks callback") {
		t.Fatalf("err = %v, want listTasks misuse", err)
	}
}

func TestPickSource_Task_EmptyList(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceTask}
	listTasks := func() ([]tasks.Task, error) { return nil, nil }
	_, err := PickSource(t.Context(), ui, []Source{SourceTask}, listTasks, nil)
	if err == nil || !strings.Contains(err.Error(), "no tasks") {
		t.Fatalf("err = %v, want 'no tasks'", err)
	}
}

func TestPickSource_Task_EmptyList_CustomError(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceTask}
	listTasks := func() ([]tasks.Task, error) { return nil, nil }
	want := errors.New("plan: no tasks to re-plan; run `j plan` first")
	_, err := PickSource(t.Context(), ui, []Source{SourceTask}, listTasks, want)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want supplied empty-tasks error", err)
	}
}

func TestPickSource_Task_ListError(t *testing.T) {
	ui := &scriptedSourceUI{source: SourceTask}
	want := errors.New("list boom")
	listTasks := func() ([]tasks.Task, error) { return nil, want }
	_, err := PickSource(t.Context(), ui, []Source{SourceTask}, listTasks, nil)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrapped 'list boom'", err)
	}
}

func TestPickSource_SelectSourceError(t *testing.T) {
	want := errors.New("source boom")
	ui := &scriptedSourceUI{sourceErr: want}
	_, err := PickSource(t.Context(), ui, []Source{SourceMarkdown}, nil, nil)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrapped 'source boom'", err)
	}
}

func TestPickSource_MarkdownError(t *testing.T) {
	want := errors.New("md boom")
	ui := &scriptedSourceUI{source: SourceMarkdown, markdownErr: want}
	_, err := PickSource(t.Context(), ui, []Source{SourceMarkdown}, nil, nil)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrapped 'md boom'", err)
	}
}
