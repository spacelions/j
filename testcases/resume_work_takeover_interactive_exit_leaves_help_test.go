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

// TestCase_ResumeTakeover_InteractiveExit_LeavesHelp pins acceptance
// criteria 1–3: a clean interactive worker exit without a PR URL
// leaves the task at status=help (not work-done) and records a
// WorkEndAt timestamp so the row is visibly terminal.
//
// This is the guard that prevents the verifier from firing on the
// same orchestrator run: rowIsNotWorkDone returns true for help, so
// skipVerifyUnlessWorkDone skips the inner verifier agent.
func TestCase_ResumeTakeover_InteractiveExit_LeavesHelp(
	t *testing.T,
) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	tasks.ResetHooksForTest()
	t.Cleanup(tasks.ResetHooksForTest)

	logPath := filepath.Join(t.TempDir(), "agent.log")
	if err := os.WriteFile(logPath,
		[]byte("no PR created here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	id := tasks.NewTaskID()
	seed := tasks.Task{
		ID:                id,
		Status:            tasks.StatusWorking,
		Summary:           "fix the thing",
		WorkTool:          "cursor",
		WorkModel:         "sonnet-4",
		WorkResumeSession: "prior-session-id",
	}
	tasks.PersistWarn(io.Discard, seed)

	// BeginWorkResume simulates the resume-work takeover path:
	// interactive=true because the user took over the session.
	lc := lifecycle.BeginWorkResume(seed, io.Discard, logPath, true)

	// Worker exits cleanly — no error, no PR URL produced.
	lc.Finish(nil)

	got := readResumeTakeoverTask(t, id)
	if got.Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help (interactive quit without PR)",
			got.Status)
	}
	if got.PullRequestURL != "" {
		t.Fatalf("PullRequestURL = %q, want empty", got.PullRequestURL)
	}
	if got.WorkEndAt.IsZero() {
		t.Fatalf("WorkEndAt should be stamped after finish")
	}
	if !got.DoneAt.IsZero() {
		t.Fatalf("DoneAt should remain zero for non-completed task")
	}
}

func readResumeTakeoverTask(t *testing.T, id string) tasks.Task {
	t.Helper()
	dir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("tasks.DefaultDir: %v", err)
	}
	s := tasks.Open(dir)
	defer func() { _ = s.Close() }()
	row, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask(%q): %v", id, err)
	}
	return row
}
