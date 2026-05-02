package prompts

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/workflow/agents/coder"
)

func TestBuildCoder(t *testing.T) {
	got := BuildCoder("/tmp/feature.plan.md", "1. step one\n2. step two", "", nil)

	if !strings.Contains(got, strings.TrimSpace(coder.Instruction)) {
		t.Fatalf("prompt missing coder.Instruction: %q", got)
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

// TestBuildCoder_WithMustread pins the bulleted must-read block on the
// coder prompt: it appears once, preserves case, and sits between the
// coder instruction and the plan body.
func TestBuildCoder_WithMustread(t *testing.T) {
	got := BuildCoder("/tmp/feature.plan.md", "body", "", []string{"AGENTS.md", "CLAUDE.md"})
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

// TestBuildCoder_TrimsLeadingTrailingWhitespace confirms that excess
// whitespace at the start of the embedded instruction does not bleed
// into the rendered prompt — the coder.Instruction value is trimmed
// before composition.
func TestBuildCoder_TrimsLeadingTrailingWhitespace(t *testing.T) {
	got := BuildCoder("p.md", "x", "", nil)
	if strings.HasPrefix(got, "\n") || strings.HasPrefix(got, " ") {
		t.Fatalf("prompt should not start with whitespace: %q", got[:10])
	}
}

// TestBuildCoder_WithWorktree pins the worktree-direction suffix
// appended on a non-empty worktree: the trailing line names the
// worktree verbatim and keeps the create-via-git-worktree-add hint.
func TestBuildCoder_WithWorktree(t *testing.T) {
	got := BuildCoder("p.md", "body", "j-my-task", nil)
	if !strings.Contains(got, "j-my-task") {
		t.Fatalf("worktree prompt missing worktree name: %q", got)
	}
	if !strings.Contains(got, "git worktree add") {
		t.Fatalf("worktree prompt missing `git worktree add` hint: %q", got)
	}
}

// TestBuildCoderResume pins the resume-only coder prompt (AC#5b):
// non-empty, mentions previous/check/continue, embeds the plan path
// and body, omits coder.Instruction, and differs from BuildCoder.
func TestBuildCoderResume(t *testing.T) {
	const planPath = "/tmp/feature.plan.md"
	const body = "1. step one\n2. step two"
	got := BuildCoderResume(planPath, body, "")
	if got == "" {
		t.Fatal("BuildCoderResume returned empty string")
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
	if strings.Contains(got, strings.TrimSpace(coder.Instruction)) {
		t.Fatalf("resume prompt should NOT include coder.Instruction: %q", got)
	}
	if got == BuildCoder(planPath, body, "", nil) {
		t.Fatal("resume prompt should differ from BuildCoder output")
	}
	if strings.Contains(strings.ToLower(got), "git worktree") {
		t.Fatalf("empty worktree should not emit worktree line: %q", got)
	}
}

// TestBuildCoderResume_WithWorktree pins the worktree-direction suffix
// on the resume path, mirroring TestBuildCoder_WithWorktree.
func TestBuildCoderResume_WithWorktree(t *testing.T) {
	got := BuildCoderResume("p.md", "body", "j-my-task")
	if !strings.Contains(got, "j-my-task") {
		t.Fatalf("resume worktree prompt missing worktree name: %q", got)
	}
	if !strings.Contains(got, "git worktree add") {
		t.Fatalf("resume worktree prompt missing `git worktree add` hint: %q", got)
	}
}
