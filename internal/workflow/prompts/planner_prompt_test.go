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

// TestBuildPlannerResume pins the resume-only planner prompt (AC#5b):
// the rendered text must be non-empty, mention "previous", "check",
// and "continue" semantics (case-insensitive), embed the supplied
// path / body for context, and explicitly NOT include
// planner.Instruction. It must also differ from BuildPlanner so the
// resume turn is observably distinct from the first run.
func TestBuildPlannerResume(t *testing.T) {
	const target = "/tmp/feature.md"
	const body = "# task\nbody"
	got := BuildPlannerResume(target, body)
	if got == "" {
		t.Fatal("BuildPlannerResume returned empty string")
	}
	lower := strings.ToLower(got)
	for _, marker := range []string{"previous", "check", "continue"} {
		if !strings.Contains(lower, marker) {
			t.Fatalf("resume prompt missing marker %q: %q", marker, got)
		}
	}
	if !strings.Contains(got, target) {
		t.Fatalf("resume prompt missing target path: %q", got)
	}
	if !strings.Contains(got, "# task") {
		t.Fatalf("resume prompt missing body: %q", got)
	}
	if strings.Contains(got, strings.TrimSpace(planner.Instruction)) {
		t.Fatalf("resume prompt should NOT include planner.Instruction: %q", got)
	}
	if got == BuildPlanner(target, body) {
		t.Fatal("resume prompt should differ from BuildPlanner output")
	}
}
