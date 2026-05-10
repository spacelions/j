package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

func TestInitRegistersMarkersHook(t *testing.T) {
	tasks.ResetHooksForTest()
	t.Cleanup(tasks.ResetHooksForTest)
	Init()
	logPath := filepath.Join(t.TempDir(), "agent.log")
	tasks.Notify(
		tasks.Transition{Event: tasks.EventPlanBegin},
		tasks.Task{ID: "x", AgentLogPath: logPath},
	)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "plan begin") {
		t.Fatalf("missing plan marker in %q", data)
	}
}

// TestMarkers_ReaperAndStuckEvents pins one marker line per reaper
// event plus EventVerifyStuck. The hook is exercised directly so the
// table is the single source of truth — adding a new reaper event
// without an entry here surfaces as a test failure rather than a
// silently-missing marker line.
func TestMarkers_ReaperAndStuckEvents(t *testing.T) {
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
			"reaper_verify_needs_clarification",
			tasks.EventReaperVerifyNeedsClarification,
			"verify needs clarification",
		},
		{
			"verify_stuck",
			tasks.EventVerifyStuck,
			"verify stuck",
		},
		{"plan_begin", tasks.EventPlanBegin, "plan begin"},
		{"plan_restart", tasks.EventPlanRestart, "plan restart"},
		{"plan_done", tasks.EventPlanDone, "plan done"},
		{
			"plan_await_approval",
			tasks.EventPlanAwaitApproval,
			"plan await approval",
		},
		{"plan_approve", tasks.EventPlanApprove, "plan approve"},
		{"plan_resume", tasks.EventPlanResume, "plan resume"},
		{"plan_quit", tasks.EventPlanQuit, "plan quit"},
		{"plan_error", tasks.EventPlanError, "plan error"},
		{
			"plan_needs_clarification",
			tasks.EventPlanNeedsClarification,
			"plan needs clarification",
		},
		{"work_begin", tasks.EventWorkBegin, "work begin"},
		{"work_restart", tasks.EventWorkRestart, "work restart"},
		{"work_resume", tasks.EventWorkResume, "work resume"},
		{"work_done", tasks.EventWorkDone, "work done"},
		{"work_quit", tasks.EventWorkQuit, "work quit"},
		{"work_error", tasks.EventWorkError, "work error"},
		{
			"work_needs_clarification",
			tasks.EventWorkNeedsClarification,
			"work needs clarification",
		},
		{"verify_begin", tasks.EventVerifyBegin, "verify begin"},
		{"verify_restart", tasks.EventVerifyRestart, "verify restart"},
		{"verify_resume", tasks.EventVerifyResume, "verify resume"},
		{"verify_pass", tasks.EventVerifyPass, "verify pass"},
		{"verify_fail", tasks.EventVerifyFail, "verify fail"},
		{"verify_quit", tasks.EventVerifyQuit, "verify quit"},
		{"verify_error", tasks.EventVerifyError, "verify error"},
		{
			"verify_needs_clarification",
			tasks.EventVerifyNeedsClarification,
			"verify needs clarification",
		},
		{"unknown", tasks.Event("custom"), "custom "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "agent.log")
			task := tasks.Task{ID: "x", AgentLogPath: logPath}
			markersHook(tasks.Transition{Event: c.event}, task)
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("read log: %v", err)
			}
			body := string(data)
			if !strings.Contains(body, c.want) {
				t.Fatalf("missing %q in %q", c.want, body)
			}
			if lines := strings.Count(
				strings.TrimSpace(body), "\n",
			); lines != 0 {
				t.Fatalf("want one marker line, got %d in %q",
					lines+1, body)
			}
		})
	}
}
