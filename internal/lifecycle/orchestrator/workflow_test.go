package orchestrator

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

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
		{"from work", "from-work", RunPhaseFromWork},
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
	if !strings.Contains(err.Error(), "want full|from-work|verify-only") {
		t.Fatalf("error = %q", err)
	}
}
