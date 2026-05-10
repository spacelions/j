package tasks

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spacelions/j/internal/lifecycle/orchestrator"
	storetasks "github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

func TestPhaseTagFor(t *testing.T) {
	tests := []struct {
		phase orchestrator.RunPhase
		want  string
	}{
		{orchestrator.RunPhaseFull, "planning"},
		{orchestrator.RunPhasePlanOnly, "planning"},
		{orchestrator.RunPhaseFromWork, "working"},
		{orchestrator.RunPhaseWorkOnly, "working"},
		{orchestrator.RunPhaseVerifyOnly, "verifying"},
		{"unknown", "planning"},
	}
	for _, tc := range tests {
		if got := phaseTagFor(tc.phase); got != tc.want {
			t.Fatalf("phaseTagFor(%q) = %q, want %q",
				tc.phase, got, tc.want)
		}
	}
}

func TestPhaseForStatus(t *testing.T) {
	tests := []struct {
		status storetasks.TaskStatus
		want   string
	}{
		{storetasks.StatusPlanning, "planning"},
		{storetasks.StatusPlanPendingApproval, "planning"},
		{storetasks.StatusPlanDone, "planning"},
		{storetasks.StatusWorking, "working"},
		{storetasks.StatusWorkDone, "working"},
		{storetasks.StatusVerifying, "verifying"},
		{storetasks.StatusFailed, "verifying"},
		{storetasks.StatusCompleted, "verifying"},
		{storetasks.StatusHelp, "fallback"},
	}
	for _, tc := range tests {
		got := phaseForStatus(tc.status, "fallback")
		if got != tc.want {
			t.Fatalf("phaseForStatus(%q) = %q, want %q",
				tc.status, got, tc.want)
		}
	}
}

func TestContentionMessageNamesTakeoverCommand(t *testing.T) {
	tests := []struct {
		phase string
		want  string
	}{
		{"planning", "resume-plan"},
		{"working", "resume-work"},
		{"verifying", "resume-verify"},
	}
	for _, tc := range tests {
		holder := storetasks.Holder{
			PID:       123,
			Host:      "host",
			Phase:     tc.phase,
			StartedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		}
		got := contentionMessage("task-1", holder)
		if !strings.Contains(got, tc.want) {
			t.Fatalf("contentionMessage(%q) = %q, want %q",
				tc.phase, got, tc.want)
		}
	}
}

func TestContentionMessageUsesCurrentTaskStatus(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	id := testutil.SeedFullTask(t, func(task *storetasks.Task) {
		task.Status = storetasks.StatusVerifying
	})
	holder := storetasks.Holder{
		PID:       os.Getpid(),
		Host:      "host",
		Phase:     "working",
		StartedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	got := contentionMessage(id, holder)
	if !strings.Contains(got, "phase: verifying") ||
		!strings.Contains(got, "resume-verify") {
		t.Fatalf("contentionMessage = %q, want current verifying row", got)
	}
}
