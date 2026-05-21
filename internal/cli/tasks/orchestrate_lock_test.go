package tasks

import (
	"os"
	"strings"
	"syscall"
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

func TestContentionMessagePreservesTerminalHolderPhase(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	id := testutil.SeedFullTask(t, func(task *storetasks.Task) {
		task.Status = storetasks.StatusFailed
	})
	holder := storetasks.Holder{
		PID:       os.Getpid(),
		Host:      "host",
		Phase:     "working",
		StartedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	got := contentionMessage(id, holder)
	if !strings.Contains(got, "phase: working") ||
		!strings.Contains(got, "resume-work") {
		t.Fatalf("contentionMessage = %q, want working resume hint", got)
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

// TestInstallOrchestrateSignalHandler_StopCleansUp covers the done
// branch in the goroutine: calling the stop function closes done and
// cancels the derived context.
func TestInstallOrchestrateSignalHandler_StopCleansUp(t *testing.T) {
	ctx, stop := installOrchestrateSignalHandler(t.Context())
	if ctx.Err() != nil {
		t.Fatal("context should not be cancelled before stop")
	}
	stop()
	select {
	case <-ctx.Done():
		// Expected: stop() cancelled the derived context.
	case <-t.Context().Done():
		t.Fatal("test context expired before handler context was cancelled")
	}
}

// TestInstallOrchestrateSignalHandler_SigTermCancels tests the signal
// branch: sending SIGTERM to self cancels the derived context.
func TestInstallOrchestrateSignalHandler_SigTermCancels(t *testing.T) {
	ctx, stop := installOrchestrateSignalHandler(t.Context())
	defer stop()
	// Send SIGTERM to the current process to trigger the signal branch.
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		stop()
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		// Expected: SIGTERM cancelled the context.
	case <-t.Context().Done():
		t.Fatal("test context expired before signal was received")
	}
}

func TestContentionMessageUsesHolderPhaseOverTaskStatus(t *testing.T) {
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
	if !strings.Contains(got, "phase: working") ||
		!strings.Contains(got, "resume-work") {
		t.Fatalf("contentionMessage = %q, want holder working row", got)
	}
}
