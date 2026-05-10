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

// TestCase_ResumeTakeover_HeadlessExit_StillWorkDone pins acceptance
// criterion 5 (regression guard): a non-interactive (headless) worker
// exit without a PR URL still produces work-done, not help. The fix
// must not change existing headless behavior.
func TestCase_ResumeTakeover_HeadlessExit_StillWorkDone(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	tasks.ResetHooksForTest()
	t.Cleanup(tasks.ResetHooksForTest)

	logPath := filepath.Join(t.TempDir(), "agent.log")
	if err := os.WriteFile(logPath,
		[]byte("no PR in this run\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	id := tasks.NewTaskID()
	seed := tasks.Task{
		ID:        id,
		Status:    tasks.StatusPlanDone,
		Summary:   "existing task",
		WorkTool:  "cursor",
		WorkModel: "sonnet-4",
	}
	tasks.PersistWarn(io.Discard, seed)

	// interactive=false: the orchestrator-driven headless case.
	lc := lifecycle.BeginWorkRestart(
		seed, io.Discard, "cursor", "sonnet-4", "", logPath, false)
	lc.Finish(nil)

	got := readResumeTakeoverTask(t, id)
	if got.Status != tasks.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done (headless, no PR still "+
			"reaches work-done)", got.Status)
	}
	if got.PullRequestURL != "" {
		t.Fatalf("PullRequestURL = %q, want empty", got.PullRequestURL)
	}
}
