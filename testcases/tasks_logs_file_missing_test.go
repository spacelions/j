package testcases_test

import (
	"strings"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksLogs_AgentLogMissing pins the acceptance bullet:
// `j tasks logs --from-task <known>` with no agent.log on disk
// prints `J: agent.log not found for task <id>` and exits 0 without
// execing `tail`.
func TestTasksLogs_AgentLogMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if err := s.PutTask(tasks.Task{
		ID:        "id-no-log",
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

	stdout, _, err := testutil.RunCobra(
		clitasks.New(), "logs", "--from-task", "id-no-log",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "J: " + tasks.AgentLogFileName +
		" not found for task id-no-log"
	if !strings.Contains(stdout, want) {
		t.Fatalf("stdout = %q, want substring %q", stdout, want)
	}
}
