package testcases_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestVerify_MarkersFireForReaperAndStuckTransitions pins acceptance
// criterion 2: marker lines now appear in agent.log for every reaper-
// driven transition AND for the stuck-verify transition. Before the
// migration, those events bypassed Notify so the marker hook never
// saw them and the per-task agent.log was missing the corresponding
// "<phase> <verb>" line. The test wires the production lifecycle.Init
// hook (the only registered observer in this binary) and feeds it a
// transition for every event the requirements call out.
func TestVerify_MarkersFireForReaperAndStuckTransitions(t *testing.T) {
	t.Cleanup(tasks.ResetHooksForTest)
	tasks.ResetHooksForTest()
	lifecycle.Init()

	cases := []struct {
		name  string
		event tasks.Event
		want  string
	}{
		{
			"reaper_plan_done",
			tasks.EventReaperPlanDone,
			"plan done",
		},
		{
			"reaper_plan_await_approval",
			tasks.EventReaperPlanAwaitApproval,
			"plan await approval",
		},
		{
			"reaper_plan_fail",
			tasks.EventReaperPlanFail,
			"plan fail",
		},
		{
			"reaper_plan_needs_clarification",
			tasks.EventReaperPlanNeedsClarification,
			"plan needs clarification",
		},
		{
			"reaper_work_done",
			tasks.EventReaperWorkDone,
			"work done",
		},
		{
			"reaper_work_needs_clarification",
			tasks.EventReaperWorkNeedsClarification,
			"work needs clarification",
		},
		{
			"verify_stuck",
			tasks.EventVerifyStuck,
			"verify stuck",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "agent.log")
			task := tasks.Task{ID: "x", AgentLogPath: logPath}
			tasks.Notify(
				tasks.Transition{Event: c.event}, task,
			)
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("read log: %v", err)
			}
			body := string(data)
			if !strings.Contains(body, c.want) {
				t.Fatalf("missing %q in %q", c.want, body)
			}
			if got := strings.Count(
				strings.TrimSpace(body), "\n",
			); got != 0 {
				t.Fatalf("want one marker line, got %d in %q",
					got+1, body)
			}
		})
	}
}
