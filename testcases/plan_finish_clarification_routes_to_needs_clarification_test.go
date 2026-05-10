package testcases_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/cli/initcmd"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestPlanFinish_ClarificationPresent_LandsNeedsClarification pins
// acceptance criterion 4 from plan.md: when the planner exits cleanly
// after writing only `clarification.md`, PlanLifecycle.Finish must
// route the row to `needs-clarification` (not `plan-done`) so
// linear_push.go never tries to upload a missing plan.md.
func TestPlanFinish_ClarificationPresent_LandsNeedsClarification(
	t *testing.T,
) {
	t.Chdir(t.TempDir())
	mustRead := ""
	if err := initcmd.Run(t.Context(), initcmd.Options{
		Yes: true, MustRead: &mustRead,
		Stdin: nil, Stdout: io.Discard, Stderr: io.Discard,
	}); err != nil {
		t.Fatalf("init: %v", err)
	}
	tasks.ResetHooksForTest()
	t.Cleanup(tasks.ResetHooksForTest)

	id := tasks.NewTaskID()
	lc := newPlanLifecycle(io.Discard, "cursor", "m", id,
		"/tmp/x.md", "# heading\nbody", "", "", "")

	dir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	clar := filepath.Join(dir, tasks.ClarificationFileName)
	if err := os.WriteFile(
		clar, []byte("please clarify\n"), 0o644); err != nil {
		t.Fatalf("write clarification.md: %v", err)
	}

	lc.Finish(nil, "", "", "/tmp/x.md")

	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	s := tasks.Open(tasksDir)
	defer func() { _ = s.Close() }()
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != tasks.StatusNeedsClarification {
		t.Fatalf("Status = %q, want needs-clarification", got.Status)
	}
	if got.PlanEndAt.IsZero() {
		t.Fatalf("PlanEndAt should be stamped")
	}
}

// TestPlanFinish_ClarificationAbsent_LandsPlanDone pins the negative
// pair: with no `clarification.md` on disk, the foreground exit keeps
// the historical plan-done landing. Coupled with the positive case
// above, the two pin the branch the foreground fix introduces.
func TestPlanFinish_ClarificationAbsent_LandsPlanDone(t *testing.T) {
	t.Chdir(t.TempDir())
	mustRead := ""
	if err := initcmd.Run(t.Context(), initcmd.Options{
		Yes: true, MustRead: &mustRead,
		Stdin: nil, Stdout: io.Discard, Stderr: io.Discard,
	}); err != nil {
		t.Fatalf("init: %v", err)
	}
	tasks.ResetHooksForTest()
	t.Cleanup(tasks.ResetHooksForTest)

	id := tasks.NewTaskID()
	lc := newPlanLifecycle(io.Discard, "cursor", "m", id,
		"/tmp/x.md", "# heading", "", "", "")
	lc.Finish(nil, "# heading", "## plan", "/tmp/x.md")

	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	s := tasks.Open(tasksDir)
	defer func() { _ = s.Close() }()
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != tasks.StatusPlanDone &&
		got.Status != tasks.StatusPlanPendingApproval {
		t.Fatalf("Status = %q, want plan-done or plan-pending-approval",
			got.Status)
	}
}
