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

// TestCase_ResumeTakeover_InteractiveWithPR_Completes pins acceptance
// criterion 4: when the resumed interactive worker produces a PR URL,
// the lifecycle emits EventWorkDone (not EventWorkQuit), so the task
// reaches work-done and the verifier guard allows the handoff.
func TestCase_ResumeTakeover_InteractiveWithPR_Completes(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	tasks.ResetHooksForTest()
	t.Cleanup(tasks.ResetHooksForTest)

	prURL := "https://github.com/owner/repo/pull/99"
	logPath := filepath.Join(t.TempDir(), "agent.log")
	if err := os.WriteFile(logPath,
		[]byte("Created pull request "+prURL+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	id := tasks.NewTaskID()
	seed := tasks.Task{
		ID:                id,
		Status:            tasks.StatusWorking,
		Summary:           "ship the feature",
		WorkTool:          "cursor",
		WorkModel:         "sonnet-4",
		WorkResumeSession: "active-session-id",
	}
	tasks.PersistWarn(io.Discard, seed)

	// interactive=true: user took over the session and the worker
	// completed with a PR.
	lc := lifecycle.BeginWorkResume(seed, io.Discard, logPath, true)
	lc.Finish(nil)

	got := readResumeTakeoverTask(t, id)
	if got.Status != tasks.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done (interactive with PR)",
			got.Status)
	}
	if got.PullRequestURL != prURL {
		t.Fatalf("PullRequestURL = %q, want %q",
			got.PullRequestURL, prURL)
	}
}
