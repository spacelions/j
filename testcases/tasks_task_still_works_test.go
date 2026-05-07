package testcases_test

import (
	"strings"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksTask_StillWorks verifies that `j tasks task --from-task <id>`
// still renders the resolved task's task.toml content to stdout, unchanged
// by the `read`→`show` rename.
func TestTasksTask_StillWorks(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if err := s.PutTask(tasks.Task{
		ID:        "id-task",
		Status:    tasks.StatusPlanDone,
		PlanTool:  "cursor",
		PlanModel: "sonnet-4",
		Summary:   "test task still works",
	}); err != nil {
		_ = s.Close()
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := tasks.EnsureDir("id-task"); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	stdout, _, err := testutil.RunCobra(
		clitasks.New(), "task", "--from-task", "id-task",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "id-task") {
		t.Fatalf("stdout = %q, want substring `id-task`", stdout)
	}
}
