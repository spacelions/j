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

// TestTasksLogs_NoTailFallback pins the acceptance bullet: "When
// `tail` is not on PATH, fall back to a one-shot read of the current
// file contents."
//
// The test sets PATH to an empty tempdir so `tail` (and `tspin`) are
// unresolvable, then runs `j tasks logs --from-task <id>`. The
// command must surface the current agent.log contents via the
// copyFileTo fallback and exit 0.
func TestTasksLogs_NoTailFallback(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if err := s.PutTask(tasks.Task{
		ID:        "id-fb",
		Status:    tasks.StatusPlanDone,
		PlanTool:  "cursor",
		PlanModel: "sonnet-4",
		Summary:   "no-tail fallback",
	}); err != nil {
		_ = s.Close()
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	dir, err := tasks.EnsureDir("id-fb")
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	body := "fallback one-shot content\n"
	if err := os.WriteFile(
		filepath.Join(dir, tasks.AgentLogFileName),
		[]byte(body), 0o644,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Empty tempdir — no tail, no tspin, no cat, no bat.
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)

	stdout, _, err := testutil.RunCobra(t,
		clitasks.New(), "logs", "--from-task", "id-fb",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "fallback one-shot content") {
		t.Fatalf(
			"stdout = %q, want substring %q",
			stdout, "fallback one-shot content",
		)
	}
}
