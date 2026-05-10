package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/coding-agents/claude"
)

// TestClaudeLocalBackend_SurvivesWorkflowRemoval pins acceptance
// criterion #2: removing the GitHub Claude Code review workflow
// must not remove or disable the local Claude coding-agent backend
// used by the CLI. The package must remain importable and
// claude.New() must return an agent named "claude".
func TestClaudeLocalBackend_SurvivesWorkflowRemoval(t *testing.T) {
	a := claude.New()
	if a == nil {
		t.Fatal("claude.New() returned nil")
	}
	if a.Name() != "claude" {
		t.Fatalf("claude.New().Name() = %q, want %q", a.Name(), "claude")
	}
}
