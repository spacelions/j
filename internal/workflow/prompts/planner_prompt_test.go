package prompts

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/workflow/agents/planner"
)

func TestBuildPlanner(t *testing.T) {
	got := BuildPlanner("/tmp/feature.md", nil)

	if !strings.Contains(got, strings.TrimSpace(planner.Instruction)) {
		t.Fatalf("prompt missing planner.Instruction: %q", got)
	}
	if !strings.Contains(got, "/tmp/feature.md") {
		t.Fatalf("prompt missing target path: %q", got)
	}
	if !strings.Contains(got, "Read the user request at") {
		t.Fatalf("prompt missing read-the-request directive: %q", got)
	}
	if strings.Contains(got, "User request (from") {
		t.Fatalf("prompt should not embed user-request body: %q", got)
	}
	if strings.Contains(got, "Before starting, read these project files") {
		t.Fatalf("prompt should not include must-read block when nil: %q", got)
	}
}

// TestBuildPlanner_WithMustread asserts the bulleted must-read block
// appears once, preserves case verbatim, and sits between the
// planner instruction and the read-the-request line.
func TestBuildPlanner_WithMustread(t *testing.T) {
	got := BuildPlanner("/tmp/feature.md", []string{"AGENTS.md", "CLAUDE.md"})

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
	// Block must precede the read-the-request directive.
	if strings.Index(got, header) > strings.Index(got, "Read the user request at") {
		t.Fatalf("must-read block must precede user-request line: %q", got)
	}
}

// TestBuildPlannerResume pins the resume-only planner prompt: the
// rendered text must be non-empty, embed the planner.Instruction
// body (which itself opens with the "You are the planner …" role
// sentence — so the resume turn re-anchors itself in the workflow
// without a duplicate preamble), mention the "previous / check /
// continue" semantics, cite the supplied path, and NOT inline the
// user-request body. It must also differ from BuildPlanner so the
// resume turn is observably distinct from the first run.
//
// The "do not overwrite saved requirements.md / plan.md" clause
// must NOT appear: the backend's save-and-exit suffix is now the
// single source of truth for the exit contract, and a help-status
// row whose first run skipped the artifacts must produce them on
// resume.
func TestBuildPlannerResume(t *testing.T) {
	const target = "/tmp/feature.md"
	got := BuildPlannerResume(target, nil)
	if got == "" {
		t.Fatal("BuildPlannerResume returned empty string")
	}
	if !strings.Contains(got, strings.TrimSpace(planner.Instruction)) {
		t.Fatalf("resume prompt missing planner.Instruction: %q", got)
	}
	const preamble = "You are the planner in a planner/worker/verifier workflow."
	if strings.Count(got, preamble) != 1 {
		t.Fatalf("resume prompt should contain the role preamble exactly once (no duplicate): %q", got)
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
	if !strings.Contains(got, "read it if you need context") {
		t.Fatalf("resume prompt missing read-for-context hint: %q", got)
	}
	if strings.Contains(got, "Original user request (from") {
		t.Fatalf("resume prompt should not embed user-request body: %q", got)
	}
	if strings.Contains(got, "do not overwrite") {
		t.Fatalf("resume prompt should NOT carry the do-not-overwrite clause anymore: %q", got)
	}
	if strings.Contains(got, "Before starting, read these project files") {
		t.Fatalf("resume prompt should not include must-read block when nil: %q", got)
	}
	if got == BuildPlanner(target, nil) {
		t.Fatal("resume prompt should differ from BuildPlanner output")
	}
}

// TestAppendPlannerSaveSuffix pins the canonical save-and-exit
// wording the cursor and claude backends both rely on. Cited path
// arguments must round-trip through %q quoting; the suffix must
// carry both numbered steps, the one-line-summary rule, and the
// "Then exit." terminator.
func TestAppendPlannerSaveSuffix(t *testing.T) {
	got := AppendPlannerSaveSuffix("BASE", "/tmp/req.md", "/tmp/plan.md")
	if !strings.HasPrefix(got, "BASE\n\n") {
		t.Fatalf("suffix should follow base verbatim with two newlines: %q", got)
	}
	for _, want := range []string{
		"During this session you may clarify",
		"Save the (possibly refined) requirements summary to",
		"\"/tmp/req.md\"",
		"one-line summary",
		"# Requirements",
		"Save the plan to",
		"\"/tmp/plan.md\"",
		"Then exit.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("suffix missing %q: %q", want, got)
		}
	}
}

// TestBuildPlannerResume_WithMustread mirrors
// TestBuildPlanner_WithMustread for the resume builder: the bulleted
// must-read block must appear exactly once, preserve case verbatim,
// and sit between the planner.Instruction body and the resume
// framing line ("You are resuming…").
func TestBuildPlannerResume_WithMustread(t *testing.T) {
	got := BuildPlannerResume("/tmp/feature.md", []string{"AGENTS.md", "CLAUDE.md"})

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
	if strings.Contains(got, "- agents.md") || strings.Contains(got, "- claude.md") {
		t.Fatalf("must-read block lowercased entries: %q", got)
	}
	const framing = "You are resuming a previous planning session."
	if strings.Index(got, header) > strings.Index(got, framing) {
		t.Fatalf("must-read block must precede resume framing line: %q", got)
	}
	if strings.Index(got, strings.TrimSpace(planner.Instruction)) > strings.Index(got, header) {
		t.Fatalf("must-read block must follow planner.Instruction body: %q", got)
	}
}
