package prompts

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/workflow/agents/planner"
)

func TestBuildPlanner(t *testing.T) {
	got := BuildPlanner("/tmp/feature.md", "# task\nbody")

	if !strings.Contains(got, strings.TrimSpace(planner.Instruction)) {
		t.Fatalf("prompt missing planner.Instruction: %q", got)
	}
	if !strings.Contains(got, "/tmp/feature.md") {
		t.Fatalf("prompt missing target path: %q", got)
	}
	if !strings.Contains(got, "# task") {
		t.Fatalf("prompt missing body: %q", got)
	}
}
