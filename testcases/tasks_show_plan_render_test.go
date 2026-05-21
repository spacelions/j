package testcases_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksShowPlan_RendersFile verifies that
// `j tasks show plan --from-task <id>` renders the resolved task's
// plan.md content to stdout.
func TestTasksShowPlan_RendersFile(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	s := tasks.OpenDefault()
	if err := s.PutTask(tasks.Task{
		ID:        "id-plan",
		Status:    tasks.StatusPlanDone,
		PlanTool:  "cursor",
		PlanModel: "sonnet-4",
		Summary:   "test show plan",
	}); err != nil {
		_ = s.Close()
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	dir, err := tasks.EnsureDir("id-plan")
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	body := "plan: create show command\n"
	if err := os.WriteFile(
		filepath.Join(dir, tasks.PlanFileName),
		[]byte(body), 0o644,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stdout, _, err := testutil.RunCobra(t,
		clitasks.New(), "show", "plan", "--from-task", "id-plan",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "create show command") {
		t.Fatalf("stdout = %q, want substring `create show command`",
			stdout)
	}
}
