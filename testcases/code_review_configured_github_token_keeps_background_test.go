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
)

// TestCodeReviewConfiguredGitHubTokenKeepsBackground verifies that a
// configured token preserves the existing detached parent behavior.
func TestCodeReviewConfiguredGitHubTokenKeepsBackground(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GITHUB_TOKEN", "acceptance-token-value")
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
		UI:       configuredGitHubTokenUI{},
		Agents: []codingagents.Agent{
			testutil.NewScriptedAgent(),
		},
		JBinary: testutil.NoopJBinary(t),
	})
	if err != nil {
		t.Fatalf("RunCodeReview returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "running in background") {
		t.Fatalf(
			"stdout = %q, want running in background dialog",
			stdout.String(),
		)
	}
	if strings.Contains(stdout.String(), "acceptance-token-value") {
		t.Fatalf("stdout leaked configured token: %q", stdout.String())
	}
	if strings.Contains(stderr.String(), "acceptance-token-value") {
		t.Fatalf("stderr leaked configured token: %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "no token configured") {
		t.Fatalf("stderr = %q, want no missing-token warning", stderr.String())
	}
}

type configuredGitHubTokenUI struct{}

func (configuredGitHubTokenUI) ConfirmDiscard(
	context.Context, taskstore.Task,
) (bool, error) {
	return false, errors.New("confirm discard should not be called")
}

func (configuredGitHubTokenUI) PickTask(
	context.Context, []taskstore.Task,
) (string, bool, error) {
	return "", false, errors.New("pick task should not be called")
}
