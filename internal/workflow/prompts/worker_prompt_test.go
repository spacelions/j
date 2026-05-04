package prompts

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/workflow/instructions"
)

func TestBuildWorker(t *testing.T) {
	got := BuildWorker("/tmp/feature.plan.md", "", nil)

	if !strings.Contains(got, strings.TrimSpace(instructions.Worker)) {
		t.Fatalf("prompt missing instructions.Worker: %q", got)
	}
	if !strings.Contains(got, "/tmp/feature.plan.md") {
		t.Fatalf("prompt missing plan path: %q", got)
	}
	if !strings.Contains(got, "Read the plan at") {
		t.Fatalf("prompt missing read-the-plan directive: %q", got)
	}
	if strings.Contains(got, "Plan (from") {
		t.Fatalf("prompt should not embed plan body: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "git worktree") {
		t.Fatalf("empty worktree should not emit worktree line: %q", got)
	}
	if strings.Contains(got, "Before starting, read these project files") {
		t.Fatalf("prompt should not include must-read block when nil: %q", got)
	}
}

// TestBuildWorker_WithMustRead pins the bulleted must-read block on
// the worker prompt: it appears once, preserves case, and sits
// between the worker instruction and the read-the-plan directive.
func TestBuildWorker_WithMustRead(t *testing.T) {
	got := BuildWorker("/tmp/feature.plan.md", "", []string{"AGENTS.md", "CLAUDE.md"})
	const header = "Before starting, read these project files for required context:"
	if strings.Count(got, header) != 1 {
		t.Fatalf("must-read header should appear exactly once: %q", got)
	}
	if !strings.Contains(got, "- AGENTS.md") || !strings.Contains(got, "- CLAUDE.md") {
		t.Fatalf("must-read block missing entries: %q", got)
	}
	if strings.Contains(got, "- agents.md") || strings.Contains(got, "- claude.md") {
		t.Fatalf("must-read block lowercased entries: %q", got)
	}
	if strings.Index(got, header) > strings.Index(got, "Read the plan at") {
		t.Fatalf("must-read block must precede read-the-plan line: %q", got)
	}
}

// TestBuildWorker_TrimsLeadingTrailingWhitespace confirms that excess
// whitespace at the start of the embedded instruction does not bleed
// into the rendered prompt — the instructions.Worker value is trimmed
// before composition.
func TestBuildWorker_TrimsLeadingTrailingWhitespace(t *testing.T) {
	got := BuildWorker("p.md", "", nil)
	if strings.HasPrefix(got, "\n") || strings.HasPrefix(got, " ") {
		t.Fatalf("prompt should not start with whitespace: %q", got[:10])
	}
}

// TestBuildWorker_WithWorktree pins the worktree-direction suffix
// appended on a non-empty worktree: the trailing line names the
// worktree verbatim and keeps the create-via-git-worktree-add hint.
func TestBuildWorker_WithWorktree(t *testing.T) {
	got := BuildWorker("p.md", "j-my-task", nil)
	if !strings.Contains(got, "j-my-task") {
		t.Fatalf("worktree prompt missing worktree name: %q", got)
	}
	if !strings.Contains(got, "git worktree add") {
		t.Fatalf("worktree prompt missing `git worktree add` hint: %q", got)
	}
}

// TestBuildWorkerResume pins the resume-only worker prompt: the
// rendered text must be non-empty, embed the instructions.Worker
// body (whose opening line "You are the worker …" doubles as the
// role preamble — so no duplicate sentence), mention the
// "previous / check / continue" semantics, cite the plan path
// without inlining the body, and differ from BuildWorker.
func TestBuildWorkerResume(t *testing.T) {
	const planPath = "/tmp/feature.plan.md"
	got := BuildWorkerResume(planPath, "")
	if got == "" {
		t.Fatal("BuildWorkerResume returned empty string")
	}
	if !strings.Contains(got, strings.TrimSpace(instructions.Worker)) {
		t.Fatalf("resume prompt missing instructions.Worker: %q", got)
	}
	const preamble = "You are the worker in a planner/worker/verifier workflow."
	if strings.Count(got, preamble) != 1 {
		t.Fatalf("resume prompt should contain the role preamble exactly once (no duplicate): %q", got)
	}
	lower := strings.ToLower(got)
	for _, marker := range []string{"previous", "check", "continue"} {
		if !strings.Contains(lower, marker) {
			t.Fatalf("resume prompt missing marker %q: %q", marker, got)
		}
	}
	if !strings.Contains(got, planPath) {
		t.Fatalf("resume prompt missing plan path: %q", got)
	}
	if !strings.Contains(got, "read it for context only") {
		t.Fatalf("resume prompt missing read-for-context hint: %q", got)
	}
	if strings.Contains(got, "Plan (from") {
		t.Fatalf("resume prompt should not embed plan body: %q", got)
	}
	if got == BuildWorker(planPath, "", nil) {
		t.Fatal("resume prompt should differ from BuildWorker output")
	}
	if strings.Contains(strings.ToLower(got), "git worktree") {
		t.Fatalf("empty worktree should not emit worktree line: %q", got)
	}
}

// TestBuildWorkerResume_WithWorktree pins the worktree-direction suffix
// on the resume path, mirroring TestBuildWorker_WithWorktree.
func TestBuildWorkerResume_WithWorktree(t *testing.T) {
	got := BuildWorkerResume("p.md", "j-my-task")
	if !strings.Contains(got, "j-my-task") {
		t.Fatalf("resume worktree prompt missing worktree name: %q", got)
	}
	if !strings.Contains(got, "git worktree add") {
		t.Fatalf("resume worktree prompt missing `git worktree add` hint: %q", got)
	}
}
