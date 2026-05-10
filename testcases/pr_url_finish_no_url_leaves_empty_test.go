package testcases_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestCase_PRURL_Finish_NoPRLeavesFieldEmpty pins acceptance
// criterion #4: when the work run did not produce a PR (agent.log
// has no GitHub URL and no branch is wired so the gh fallback is a
// no-op), pull_request_url stays empty and the FSM still reaches
// work-done. This guards against a regression where the new
// detection might falsely synthesise a URL or otherwise destabilise
// the no-PR happy path.
func TestCase_PRURL_Finish_NoPRLeavesFieldEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	tasks.ResetHooksForTest()
	t.Cleanup(tasks.ResetHooksForTest)

	logPath := filepath.Join(t.TempDir(), "agent.log")
	if err := os.WriteFile(logPath,
		[]byte("did some work, no PR\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lc := newWorkLifecycle(io.Discard, "cursor", "sonnet-4",
		tasks.NewTaskID(), "/tmp/x.plan.md", "", "body", "",
		logPath)
	pre := lc.Task()
	if pre.PullRequestURL != "" {
		t.Fatalf("pre PullRequestURL = %q, want empty",
			pre.PullRequestURL)
	}
	lc.Finish(nil)

	got := readSinglePersistedTaskNoURL(t)
	if got.PullRequestURL != "" {
		t.Fatalf("PullRequestURL = %q, want empty",
			got.PullRequestURL)
	}
	if got.Status != tasks.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
	}
}

func readSinglePersistedTaskNoURL(t *testing.T) tasks.Task {
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
