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

// TestTasksLogs_RendersAgentLog pins the acceptance bullet:
// `j tasks logs --from-task <id>` renders the resolved task's
// agent.log via the bat/cat viewer (one-shot, no `tail -f`) and the
// rendered bytes appear on stdout, identical to `j tasks show
// requirements`.
func TestTasksLogs_RendersAgentLog(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if err := s.PutTask(tasks.Task{
		ID:        "id-render",
		Status:    tasks.StatusPlanDone,
		PlanTool:  "cursor",
		PlanModel: "sonnet-4",
		Summary:   "logs render via viewer",
	}); err != nil {
		_ = s.Close()
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	dir, err := tasks.EnsureDir("id-render")
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	body := "agentlog: rendered via viewer\n"
	if err := os.WriteFile(
		filepath.Join(dir, tasks.AgentLogFileName),
		[]byte(body), 0o644,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stdout, _, err := testutil.RunCobra(
		clitasks.New(), "logs", "--from-task", "id-render",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "rendered via viewer") {
		t.Fatalf(
			"stdout = %q, want substring `rendered via viewer`",
			stdout,
		)
	}
}
