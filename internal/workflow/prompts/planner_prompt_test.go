package prompts

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/workflow/agents/planner"
)

func TestBuildPlanner(t *testing.T) {
	got := BuildPlanner("/tmp/feature.md", "# task\nbody", nil)

	if !strings.Contains(got, strings.TrimSpace(planner.Instruction)) {
		t.Fatalf("prompt missing planner.Instruction: %q", got)
	}
	if !strings.Contains(got, "/tmp/feature.md") {
		t.Fatalf("prompt missing target path: %q", got)
	}
	if !strings.Contains(got, "# task") {
		t.Fatalf("prompt missing body: %q", got)
	}
	if strings.Contains(got, "Before starting, read these project files") {
		t.Fatalf("prompt should not include must-read block when nil: %q", got)
	}
}

// TestBuildPlanner_WithMustread asserts the bulleted must-read block
// appears once, preserves case verbatim, and sits between the
// planner instruction and the user-request section.
func TestBuildPlanner_WithMustread(t *testing.T) {
	got := BuildPlanner("/tmp/feature.md", "# task\nbody", []string{"AGENTS.md", "CLAUDE.md"})

	const header = "Before starting, read these project files for required context:"
	if strings.Count(got, header) != 1 {
		t.Fatalf("must-read header should appear exactly once: %q", got)
	}
	if !strings.Contains(got, "- AGENTS.md") {
		t.Fatalf("must-read block missing AGENTS.md bullet: %q", got)
	}
	if !strings.Contains(got, "- CLAUDE.md") {
		t.Fatalf("must-read block missing CLAUDE.md bullet: %q", got)
	}
	// Case preservation: the lowercase forms must NOT appear.
	if strings.Contains(got, "- agents.md") || strings.Contains(got, "- claude.md") {
		t.Fatalf("must-read block lowercased entries: %q", got)
	}
	// Block must precede the user request line.
	if strings.Index(got, header) > strings.Index(got, "User request (from") {
		t.Fatalf("must-read block must precede user request: %q", got)
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
	if got == BuildPlanner(target, body, nil) {
		t.Fatal("resume prompt should differ from BuildPlanner output")
	}
}
