package prompts

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/workflow/agents/worker"
)

func TestBuildWorker(t *testing.T) {
	got := BuildWorker("/tmp/feature.plan.md", "1. step one\n2. step two", "", nil)

	if !strings.Contains(got, strings.TrimSpace(worker.Instruction)) {
		t.Fatalf("prompt missing worker.Instruction: %q", got)
	}
	if !strings.Contains(got, "/tmp/feature.plan.md") {
		t.Fatalf("prompt missing plan path: %q", got)
	}
	if !strings.Contains(got, "1. step one") {
		t.Fatalf("prompt missing body: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "git worktree") {
		t.Fatalf("empty worktree should not emit worktree line: %q", got)
	}
	if strings.Contains(got, "Before starting, read these project files") {
		t.Fatalf("prompt should not include must-read block when nil: %q", got)
	}
}

// TestBuildWorker_WithMustread pins the bulleted must-read block on the
// worker prompt: it appears once, preserves case, and sits between the
// worker instruction and the plan body.
func TestBuildWorker_WithMustread(t *testing.T) {
	got := BuildWorker("/tmp/feature.plan.md", "body", "", []string{"AGENTS.md", "CLAUDE.md"})
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
	if strings.Index(got, header) > strings.Index(got, "Plan (from") {
		t.Fatalf("must-read block must precede plan: %q", got)
	}
}

// TestBuildWorker_TrimsLeadingTrailingWhitespace confirms that excess
// whitespace at the start of the embedded instruction does not bleed
// into the rendered prompt — the worker.Instruction value is trimmed
// before composition.
func TestBuildWorker_TrimsLeadingTrailingWhitespace(t *testing.T) {
	got := BuildWorker("p.md", "x", "", nil)
	if strings.HasPrefix(got, "\n") || strings.HasPrefix(got, " ") {
		t.Fatalf("prompt should not start with whitespace: %q", got[:10])
	}
}

// TestBuildWorker_WithWorktree pins the worktree-direction suffix
// appended on a non-empty worktree: the trailing line names the
// worktree verbatim and keeps the create-via-git-worktree-add hint.
func TestBuildWorker_WithWorktree(t *testing.T) {
	got := BuildWorker("p.md", "body", "j-my-task", nil)
	if !strings.Contains(got, "j-my-task") {
		t.Fatalf("worktree prompt missing worktree name: %q", got)
	}
	if !strings.Contains(got, "git worktree add") {
		t.Fatalf("worktree prompt missing `git worktree add` hint: %q", got)
	}
}

// TestBuildWorkerResume pins the resume-only worker prompt (AC#5b):
// non-empty, mentions previous/check/continue, embeds the plan path
// and body, omits worker.Instruction, and differs from BuildWorker.
func TestBuildWorkerResume(t *testing.T) {
	const planPath = "/tmp/feature.plan.md"
	const body = "1. step one\n2. step two"
	got := BuildWorkerResume(planPath, body, "")
	if got == "" {
		t.Fatal("BuildWorkerResume returned empty string")
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
	if !strings.Contains(got, "1. step one") {
		t.Fatalf("resume prompt missing body: %q", got)
	}
	if strings.Contains(got, strings.TrimSpace(worker.Instruction)) {
		t.Fatalf("resume prompt should NOT include worker.Instruction: %q", got)
	}
	if got == BuildWorker(planPath, body, "", nil) {
		t.Fatal("resume prompt should differ from BuildWorker output")
	}
	if strings.Contains(strings.ToLower(got), "git worktree") {
		t.Fatalf("empty worktree should not emit worktree line: %q", got)
	}
}

// TestBuildWorkerResume_WithWorktree pins the worktree-direction suffix
// on the resume path, mirroring TestBuildWorker_WithWorktree.
func TestBuildWorkerResume_WithWorktree(t *testing.T) {
	got := BuildWorkerResume("p.md", "body", "j-my-task")
	if !strings.Contains(got, "j-my-task") {
		t.Fatalf("resume worktree prompt missing worktree name: %q", got)
	}
	if !strings.Contains(got, "git worktree add") {
		t.Fatalf("resume worktree prompt missing `git worktree add` hint: %q", got)
	}
}
