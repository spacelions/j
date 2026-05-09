package tasks

import (
	"strings"
	"testing"
	"time"

	"github.com/spacelions/j/internal/lifecycle/orchestrator"
	storetasks "github.com/spacelions/j/internal/store/tasks"
)

func TestPhaseTagFor(t *testing.T) {
	tests := []struct {
		phase orchestrator.RunPhase
		want  string
	}{
		{orchestrator.RunPhaseFull, "planning"},
		{orchestrator.RunPhaseFromWork, "working"},
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
