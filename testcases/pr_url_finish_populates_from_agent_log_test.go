package testcases_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestCase_PRURL_Finish_PopulatesFromAgentLog pins acceptance
// criterion #1: a successful `j work` run that produced a GitHub PR
// must persist the PR URL on the task row by the time the work-end
// transition fires. We drive WorkLifecycle.Finish(nil) with an
// agent.log that already contains a PR URL line and assert that the
// in-memory + persisted task row carries pull_request_url.
func TestCase_PRURL_Finish_PopulatesFromAgentLog(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	tasks.ResetHooksForTest()
	t.Cleanup(tasks.ResetHooksForTest)

	logPath := filepath.Join(t.TempDir(), "agent.log")
	prURL := "https://github.com/spacelions/j/pull/4242"
	if err := os.WriteFile(logPath,
		[]byte("opened "+prURL+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lc := lifecycle.NewWorkTask(io.Discard, "cursor", "sonnet-4",
		tasks.NewTaskID(), "/tmp/x.plan.md", "", "body", "",
		logPath)
	lc.Finish(nil)

	if got := lc.Task().PullRequestURL; got != prURL {
		t.Fatalf("Task().PullRequestURL = %q, want %q", got, prURL)
	}
	got := readSinglePersistedTask(t)
	if got.PullRequestURL != prURL {
		t.Fatalf("persisted PullRequestURL = %q, want %q",
			got.PullRequestURL, prURL)
	}
	if got.Status != tasks.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
	}
}

func readSinglePersistedTask(t *testing.T) tasks.Task {
	t.Helper()
	dir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("tasks.DefaultDir: %v", err)
	}
	s := tasks.Open(dir)
	defer func() { _ = s.Close() }()
	rows, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 task row, got %d", len(rows))
	}
	return rows[0]
}
