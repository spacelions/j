package prompts

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/instructions"
)

func TestBuildPlanner(t *testing.T) {
	got := BuildPlanner("/tmp/feature.md", nil)

	if !strings.Contains(got, strings.TrimSpace(instructions.Planner)) {
		t.Fatalf("prompt missing instructions.Planner: %q", got)
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

// TestBuildPlanner_WithMustRead asserts the bulleted must-read block
// appears once, preserves case verbatim, and sits between the
// planner instruction and the read-the-request line.
func TestBuildPlanner_WithMustRead(t *testing.T) {
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
// rendered text must be non-empty, embed the instructions.Planner
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
	if !strings.Contains(got, strings.TrimSpace(instructions.Planner)) {
		t.Fatalf("resume prompt missing instructions.Planner: %q", got)
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
// carry both numbered steps, the one-line-summary rule, the
// PM/QA-tone contract for requirements.md, the clarification
// escape hatch (with the per-task absolute path threaded in), and
// the "Then exit." terminator.
func TestAppendPlannerSaveSuffix(t *testing.T) {
	got := AppendPlannerSaveSuffix(
		"BASE", "/tmp/req.md", "/tmp/plan.md",
		"/tmp/clarification.md",
	)
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
		"PM/QA-style spec",
		"acceptance criteria",
		"plan.md is the technical companion",
		"\"/tmp/clarification.md\"",
		"Then exit.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("suffix missing %q: %q", want, got)
		}
	}
}

// TestAppendPlannerSaveSuffix_PMToneNoImplDetail pins the AC: the
// PM/QA-tone contract carried by the suffix explicitly forbids
// implementation detail (file paths, function signatures,
// architecture) inside requirements.md. This is the tone half of
// what the user asked for; combined with the one-line-summary rule
// it guarantees requirements.md is product-shaped.
func TestAppendPlannerSaveSuffix_PMToneNoImplDetail(t *testing.T) {
	got := AppendPlannerSaveSuffix(
		"BASE", "/tmp/req.md", "/tmp/plan.md", "/tmp/c.md",
	)
	for _, want := range []string{
		"behavioral",
		"file paths",
		"function signatures",
		"belong in plan.md",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("suffix missing PM-tone marker %q: %q", want, got)
		}
	}
}

// TestBuildPlannerClarificationResume pins the
// resume-from-clarification planner prompt: the rendered text must
// be non-empty, embed instructions.Planner, cite the
// clarification.md path twice (once to read, once to delete),
// cite the original target path, mention the
// "delete clarification.md" contract so Finish() routes to the
// natural terminal status, and differ from BuildPlannerResume.
func TestBuildPlannerClarificationResume(t *testing.T) {
	const (
		target = "/tmp/feature.md"
		clar   = "/tmp/.j/tasks/abc/clarification.md"
	)
	got := BuildPlannerClarificationResume(target, clar, nil)
	if got == "" {
		t.Fatal("BuildPlannerClarificationResume returned empty")
	}
	if !strings.Contains(got, strings.TrimSpace(instructions.Planner)) {
		t.Fatalf("prompt missing instructions.Planner: %q", got)
	}
	if strings.Count(got, clar) != 2 {
		t.Fatalf(
			"clarification path should appear twice (read+delete): %q",
			got,
		)
	}
	if !strings.Contains(got, target) {
		t.Fatalf("prompt missing target path: %q", got)
	}
	for _, want := range []string{
		"paused with an open question",
		"delete",
		"natural terminal status",
		"needs-clarification",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q: %q", want, got)
		}
	}
	if got == BuildPlannerResume(target, nil) {
		t.Fatal("clarification-resume prompt should differ from resume")
	}
}

// TestBuildPlannerClarificationResume_WithMustRead pins the
// must-read block on the new builder, mirroring its sibling
// builders.
func TestBuildPlannerClarificationResume_WithMustRead(t *testing.T) {
	got := BuildPlannerClarificationResume(
		"/tmp/feature.md", "/tmp/c.md",
		[]string{"AGENTS.md", "CLAUDE.md"},
	)
	const header = "Before starting, read these project files for required context:"
	if strings.Count(got, header) != 1 {
		t.Fatalf("must-read header should appear exactly once: %q", got)
	}
	if !strings.Contains(got, "- AGENTS.md") ||
		!strings.Contains(got, "- CLAUDE.md") {
		t.Fatalf("must-read block missing entries: %q", got)
	}
}

// TestBuildPlannerResume_WithMustRead mirrors
// TestBuildPlanner_WithMustRead for the resume builder: the bulleted
// must-read block must appear exactly once, preserve case verbatim,
// and sit at the very top — above both the instructions.Planner
// body and the resume framing line.
func TestBuildPlannerResume_WithMustRead(t *testing.T) {
	got := BuildPlannerResume(
		"/tmp/feature.md", []string{"AGENTS.md", "CLAUDE.md"},
	)

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
	if strings.Index(got, header) > strings.Index(got, strings.TrimSpace(instructions.Planner)) {
		t.Fatalf("must-read block must precede instructions.Planner body: %q", got)
	}
}
