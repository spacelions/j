package testcases_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	taskstore "github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
	"github.com/spacelions/j/internal/tools/github"
)

// TestCodeReviewMissingGitHubTokenStopsDefault verifies that the default
// parent flow reports missing authentication before detached backgrounding.
func TestCodeReviewMissingGitHubTokenStopsDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	testutil.Init(t)
	id := testutil.SeedFullTask(t, func(row *taskstore.Task) {
		row.Status = taskstore.StatusWorkDone
		row.PullRequestURL = "https://github.com/acme/app/pull/42"
	})

	var stdout, stderr bytes.Buffer
	err := clitasks.RunCodeReview(t.Context(), clitasks.CodeReviewOptions{
		FromTask: id,
		Stdout:   &stdout,
		Stderr:   &stderr,
		UI:       missingGitHubTokenUI{},
		Agents: []codingagents.Agent{
			testutil.NewScriptedAgent(),
		},
	})

	if !errors.Is(err, github.ErrTokenNotConfigured) {
		t.Fatalf("err = %v, want ErrTokenNotConfigured", err)
	}
	stderrText := stderr.String()
	for _, want := range []string{
		"J:", "github.token", "GITHUB_TOKEN", "GH_TOKEN",
	} {
		if !strings.Contains(stderrText, want) {
			t.Fatalf("stderr = %q, want %q", stderrText, want)
		}
	}
	if strings.Contains(stdout.String(), "running in background") {
		t.Fatalf("stdout = %q, want no background dialog", stdout.String())
	}
	if strings.Contains(stderrText, "ghp_") ||
		strings.Contains(stdout.String(), "ghp_") {
		t.Fatalf("output should not contain token-like values")
	}
}

type missingGitHubTokenUI struct{}

func (missingGitHubTokenUI) ConfirmDiscard(
	context.Context, taskstore.Task,
) (bool, error) {
	return false, errors.New("confirm discard should not be called")
}

func (missingGitHubTokenUI) PickTask(
	context.Context, []taskstore.Task,
) (string, bool, error) {
	return "", false, errors.New("pick task should not be called")
}
