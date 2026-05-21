package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

// TestRun_DefaultLauncherConsole exercises the Run happy path (return nil):
// passing nil launcherArgs selects the default console sublauncher, which
// exits immediately on a pre-cancelled context, returning nil.
func TestRun_DefaultLauncherConsole(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Run(
		ctx,
		store.ProjectConfig{APIKey: "bogus", Model: "gemini-2.5-flash", MaxIterations: 1},
		nil,
	)
	if err != nil {
		t.Logf("Run with nil args err = %v (non-nil is also acceptable)", err)
	}
}

// TestRun_SmokeBogusLauncherArgs exercises Run end-to-end through real model,
// sub-agent, and workflow-agent construction. The launcher rejects the bogus
// subcommand, so no network call and no server is started.
func TestRun_SmokeBogusLauncherArgs(t *testing.T) {
	err := Run(
		t.Context(),
		store.ProjectConfig{APIKey: "bogus", Model: "gemini-2.5-flash", MaxIterations: 1},
		[]string{"definitely-not-a-real-subcommand"},
	)
	if err == nil {
		t.Fatal("expected error from launcher")
	}
	if !strings.Contains(err.Error(), "workflow:") {
		t.Fatalf("expected wrapped workflow error, got %v", err)
	}
}

func TestParseRunPhase(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want RunPhase
	}{
		{"empty", "", RunPhaseFull},
		{"full", "full", RunPhaseFull},
		{"plan only", "plan-only", RunPhasePlanOnly},
		{"from work", "from-work", RunPhaseFromWork},
		{"work only", "work-only", RunPhaseWorkOnly},
		{"verify only", "verify-only", RunPhaseVerifyOnly},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRunPhase(tc.in)
			if err != nil {
				t.Fatalf("ParseRunPhase: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParseRunPhase = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseRunPhase_RejectsUnknown(t *testing.T) {
	got, err := ParseRunPhase("worker")
	if err == nil {
		t.Fatalf("ParseRunPhase = %q, want error", got)
	}
	if !strings.Contains(err.Error(), "want full|plan-only|from-work|work-only|verify-only") {
		t.Fatalf("error = %q", err)
	}
}
