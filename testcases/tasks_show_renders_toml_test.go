package testcases_test

import (
	"strings"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksShow_RendersTaskToml verifies that `j tasks show --from-task <id>`
// renders the resolved task's task.toml content to stdout.
func TestTasksShow_RendersTaskToml(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if err := s.PutTask(tasks.Task{
		ID:        "id-show",
		Status:    tasks.StatusPlanDone,
		PlanTool:  "cursor",
		PlanModel: "sonnet-4",
		Summary:   "test show render",
	}); err != nil {
		_ = s.Close()
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := tasks.EnsureDir("id-show"); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	stdout, _, err := testutil.RunCobra(t,
		clitasks.New(), "show", "--from-task", "id-show",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "id-show") {
		t.Fatalf("stdout = %q, want substring `id-show`", stdout)
	}
}
