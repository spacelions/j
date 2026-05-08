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

// TestTasksShowFindings_RendersFile pins acceptance criterion #2:
// `j tasks show findings --from-task <id>` renders the resolved
// task's verifier_findings.md content to stdout via the shared
// viewer pipeline.
func TestTasksShowFindings_RendersFile(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if err := s.PutTask(tasks.Task{
		ID:        "id-find",
		Status:    tasks.StatusPlanDone,
		PlanTool:  "cursor",
		PlanModel: "sonnet-4",
		Summary:   "test show findings",
	}); err != nil {
		_ = s.Close()
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	dir, err := tasks.EnsureDir("id-find")
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	body := "verdict bullets\nVERDICT: PASS\n"
	if err := os.WriteFile(
		filepath.Join(dir, tasks.VerifierFindingsFileName),
		[]byte(body), 0o644,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stdout, _, err := testutil.RunCobra(t,
		clitasks.New(), "show", "findings",
		"--from-task", "id-find",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "VERDICT: PASS") {
		t.Fatalf("stdout = %q, want substring `VERDICT: PASS`",
			stdout)
	}
}
