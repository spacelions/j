package testcases_test

import (
	"strings"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksShowRequirements_FileMissing pins the acceptance bullet:
// with `--from-task <known-id>` and the relevant file absent under
// the task dir, the leaf prints `J: requirements.md not found for
// task <id>` and exits 0 with no renderer subprocess.
func TestTasksShowRequirements_FileMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	s := tasks.OpenDefault()
	if err := s.PutTask(tasks.Task{
		ID:        "id-no-file",
		Status:    tasks.StatusPlanDone,
		PlanTool:  "cursor",
		PlanModel: "sonnet-4",
		Summary:   "x",
	}); err != nil {
		_ = s.Close()
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := tasks.EnsureDir("id-no-file"); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	stdout, _, err := testutil.RunCobra(t,
		clitasks.New(), "show", "requirements",
		"--from-task", "id-no-file",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "J: " + tasks.RequirementsFileName +
		" not found for task id-no-file"
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout = %q, want substring %q", stdout, want)
	}
}
