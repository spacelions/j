package testcases_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// emptyAssignedIssuesUI is the smallest picker.SourceUI that drives
// PickSource straight into pickLinearSource: SelectSource returns
// SourceLinear; the Linear sub-flow needs no UI prompts because the
// API key + project are already persisted in the test's fresh store.
type emptyAssignedIssuesUI struct {
	pickedIssueCalls int
}

func (u *emptyAssignedIssuesUI) SelectSource(
	_ context.Context, _ []picker.Source,
) (picker.Source, error) {
	return picker.SourceLinear, nil
}

func (u *emptyAssignedIssuesUI) PickMarkdownInCwd(
	_ context.Context,
) (string, error) {
	return "", nil
}

func (u *emptyAssignedIssuesUI) PickTask(
	_ context.Context, _ string, _ []tasks.Task,
) (string, bool, error) {
	return "", false, nil
}

func (u *emptyAssignedIssuesUI) PromptLinearAPIKey(
	_ context.Context, _ string,
) (string, bool, error) {
	return "", false, nil
}

func (u *emptyAssignedIssuesUI) PickLinearProject(
	_ context.Context, _ []linear.Project,
) (linear.Project, bool, error) {
	return linear.Project{}, false, nil
}

func (u *emptyAssignedIssuesUI) PickLinearIssue(
	_ context.Context, _ []linear.Issue,
) (linear.Issue, bool, error) {
	u.pickedIssueCalls++
	return linear.Issue{}, false, nil
}

// stubEmptyAssignedIssues points linear.TestEndpoint at a server that
// returns an empty viewer.assignedIssues.nodes payload. The endpoint
// override is reset on t.Cleanup. Replicates the unexported helper in
// internal/cli/picker so this test can run from package testcases_test
// without crossing the picker package boundary.
func stubEmptyAssignedIssues(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"viewer": map[string]any{
						"assignedIssues": map[string]any{
							"nodes": []any{},
						},
					},
				},
			})
		}))
	t.Cleanup(srv.Close)
	prev := linear.TestEndpoint
	linear.TestEndpoint = srv.URL
	t.Cleanup(func() { linear.TestEndpoint = prev })
}

// TestLinearPickerEmptyAssignedMessage pins SPA-54: the picker's
// empty-assigned-issues branch returns the simplified message
// `no Linear issues assigned to you.` exactly, with no `picker:`
// prefix and no `(use --from-linear ...)` parenthetical hint.
//
// Black-box: drives picker.PickSource against a stubbed empty-list
// Linear endpoint and asserts on the returned error string.
func TestLinearPickerEmptyAssignedMessage(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := linear.SaveAPIKey("lin_api_test"); err != nil {
		t.Fatalf("SaveAPIKey: %v", err)
	}
	if err := linear.SaveProject("p"); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}
	stubEmptyAssignedIssues(t)

	ui := &emptyAssignedIssuesUI{}
	_, err := picker.PickSource(
		context.Background(), ui,
		[]picker.Source{picker.SourceLinear}, nil, nil,
	)
	if err == nil {
		t.Fatalf("err = nil, want empty-list error")
	}
	got := err.Error()

	const want = "no Linear issues assigned to you."
	if got != want {
		t.Fatalf("err = %q, want exact %q", got, want)
	}

	if strings.Contains(got, "picker:") {
		t.Fatalf("err %q still has the dropped picker: prefix", got)
	}
	if strings.Contains(got, "--from-linear") {
		t.Fatalf("err %q still has the dropped --from-linear hint", got)
	}
	if strings.Contains(got, "assign yourself") {
		t.Fatalf("err %q still has the dropped assign-yourself hint", got)
	}

	if ui.pickedIssueCalls != 0 {
		t.Fatalf(
			"PickLinearIssue should not fire on empty list: calls=%d",
			ui.pickedIssueCalls,
		)
	}
}
