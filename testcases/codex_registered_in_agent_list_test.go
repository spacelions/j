package testcases_test

import (
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/codex"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/coding-agents/deepseek"
)

// TestCodexIsSecondRegisteredAgent pins the picker / settings
// acceptance criterion specific to the codex backend: codex is the
// second tool the picker offers (slot index 1) so it sits next to
// claude in the default highlight chain. A regression that promotes
// codex to slot 0 (becoming the new highlighted default) or demotes
// it past the deepseek / cursor entries fails the build before users
// see the change.
func TestCodexIsSecondRegisteredAgent(t *testing.T) {
	agents := []codingagents.Agent{
		claude.New(), codex.New(), deepseek.New(), cursor.New(),
	}

	const wantIndex = 1
	const wantName = "codex"
	if agents[wantIndex].Name() != wantName {
		t.Fatalf(
			"agents[%d].Name() = %q, want %q",
			wantIndex, agents[wantIndex].Name(), wantName,
		)
	}
}
