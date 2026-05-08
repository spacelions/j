package testcases_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestAgentLog_ForegroundWorkNeedsClarification_MarkerLine pins
// acceptance criterion 2: a foreground worker-clarification
// transition writes a `work needs clarification` line into the
// per-task `agent.log` via the registered markers hook, mirroring
// the existing planner-foreground / reaper-driven markers.
func TestAgentLog_ForegroundWorkNeedsClarification_MarkerLine(
	t *testing.T,
) {
	t.Cleanup(tasks.ResetHooksForTest)
	tasks.ResetHooksForTest()
	lifecycle.Init()

	logPath := filepath.Join(t.TempDir(), "agent.log")
	tasks.Notify(
		tasks.Transition{
			From:  tasks.StatusWorking,
			Event: tasks.EventWorkNeedsClarification,
			To:    tasks.StatusNeedsClarification,
		},
		tasks.Task{ID: "x", AgentLogPath: logPath},
	)

	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read agent.log: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, "work needs clarification") {
		t.Fatalf("agent.log = %q, want `work needs clarification`",
			got)
	}
	if strings.Count(strings.TrimSpace(got), "\n") != 0 {
		t.Fatalf("expected a single marker line, got %q", got)
	}
}
