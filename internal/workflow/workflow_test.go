package workflow

import (
	"context"
	"strings"
	"testing"
)

// TestRun_SmokeBogusLauncherArgs exercises Run end-to-end through real model,
// sub-agent, and workflow-agent construction. The launcher rejects the bogus
// subcommand, so no network call and no server is started.
func TestRun_SmokeBogusLauncherArgs(t *testing.T) {
	err := Run(
		context.Background(),
		Config{APIKey: "bogus", Model: "gemini-2.5-flash", MaxIterations: 1},
		[]string{"definitely-not-a-real-subcommand"},
	)
	if err == nil {
		t.Fatal("expected error from launcher")
	}
	if !strings.Contains(err.Error(), "workflow:") {
		t.Fatalf("expected wrapped workflow error, got %v", err)
	}
}
