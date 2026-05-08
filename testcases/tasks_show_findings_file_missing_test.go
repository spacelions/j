package testcases_test

import (
	"strings"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksShowFindings_FileMissing pins acceptance criterion #5:
// with `--from-task <known-id>` and verifier_findings.md absent
// under the task dir, the leaf prints
// `J: verifier_findings.md not found for task <id>` and exits 0
// with no renderer subprocess.
func TestTasksShowFindings_FileMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if err := s.PutTask(tasks.Task{
		ID:        "id-no-findings",
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
	if _, err := tasks.EnsureDir("id-no-findings"); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	stdout, _, err := testutil.RunCobra(
		clitasks.New(), "show", "findings",
		"--from-task", "id-no-findings",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "J: " + tasks.VerifierFindingsFileName +
		" not found for task id-no-findings"
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout = %q, want substring %q", stdout, want)
	}
}
