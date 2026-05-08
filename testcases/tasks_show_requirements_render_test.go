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

// TestTasksShowRequirements_RendersFile verifies that
// `j tasks show requirements --from-task <id>` renders the resolved
// task's requirements.md content to stdout.
func TestTasksShowRequirements_RendersFile(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if err := s.PutTask(tasks.Task{
		ID:        "id-req",
		Status:    tasks.StatusPlanDone,
		PlanTool:  "cursor",
		PlanModel: "sonnet-4",
		Summary:   "test show requirements",
	}); err != nil {
		_ = s.Close()
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	dir, err := tasks.EnsureDir("id-req")
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	body := "requirement: rename read to show\n"
	if err := os.WriteFile(
		filepath.Join(dir, tasks.RequirementsFileName),
		[]byte(body), 0o644,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stdout, _, err := testutil.RunCobra(t,
		clitasks.New(), "show", "requirements", "--from-task", "id-req",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "rename read to show") {
		t.Fatalf("stdout = %q, want substring `rename read to show`",
			stdout)
	}
}
