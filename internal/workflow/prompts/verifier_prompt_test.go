package prompts

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/workflow/agents/verifier"
)

func TestBuildVerifier(t *testing.T) {
	const (
		reqPath          = "/tmp/.j/tasks/abc/requirements.md"
		reqBody          = "# requirements\nstuff"
		planPath         = "/tmp/.j/tasks/abc/plan.md"
		planBody         = "1. step one\n2. step two"
		verifierPlanPath = "/tmp/.j/tasks/abc/verifier_plan.md"
		findingsPath     = "/tmp/.j/tasks/abc/verifier_findings.md"
	)
	got := BuildVerifier(reqPath, reqBody, planPath, planBody, verifierPlanPath, findingsPath, "", nil)

	if !strings.Contains(got, strings.TrimSpace(verifier.Instruction)) {
		t.Fatalf("prompt missing verifier.Instruction: %q", got)
	}
	for _, want := range []string{reqPath, reqBody, planPath, planBody, findingsPath} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, verifierPlanPath) {
		t.Fatalf("prompt should not reference verifier_plan.md: %q", got)
	}
	for _, want := range []string{"VERDICT: PASS", "VERDICT: FAIL", "Then exit."} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q: %q", want, got)
		}
	}
	if strings.Contains(strings.ToLower(got), "git worktree") {
		t.Fatalf("empty worktree should not emit worktree line: %q", got)
	}
	if strings.Contains(got, "Before starting, read these project files") {
		t.Fatalf("prompt should not include must-read block when nil: %q", got)
	}
}

// TestBuildVerifier_WithMustread pins the bulleted must-read block on
// the verifier prompt: appears once, preserves case, and sits between
// the verifier instruction and the requirements section.
func TestBuildVerifier_WithMustread(t *testing.T) {
	got := BuildVerifier("r.md", "r", "p.md", "p", "vp.md", "vf.md", "", []string{"AGENTS.md", "CLAUDE.md"})
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
	if strings.Index(got, header) > strings.Index(got, "Requirements (from") {
		t.Fatalf("must-read block must precede requirements: %q", got)
	}
}

// TestBuildVerifier_TrimsLeadingWhitespace pins the same trim
// invariant exercised by the planner/coder prompts: the embedded
// verifier.Instruction must not bleed leading whitespace into the
// composed prompt.
func TestBuildVerifier_TrimsLeadingWhitespace(t *testing.T) {
	got := BuildVerifier("r.md", "r", "p.md", "p", "vp.md", "vf.md", "", nil)
	if strings.HasPrefix(got, "\n") || strings.HasPrefix(got, " ") {
		t.Fatalf("prompt should not start with whitespace: %q", got[:10])
	}
}

// TestBuildVerifier_WithWorktree pins the worktree-direction suffix
// on the first-run verifier prompt: the trailing line names the
// worktree verbatim and mentions `git worktree list` so the verifier
// knows how to resolve the absolute path.
func TestBuildVerifier_WithWorktree(t *testing.T) {
	got := BuildVerifier("r.md", "r", "p.md", "p", "vp.md", "vf.md", "j-my-task", nil)
	if !strings.Contains(got, "j-my-task") {
		t.Fatalf("worktree prompt missing worktree name: %q", got)
	}
	if !strings.Contains(got, "git worktree list") {
		t.Fatalf("worktree prompt missing `git worktree list` hint: %q", got)
	}
}

func TestBuildVerifierResume(t *testing.T) {
	const (
		reqPath  = "/tmp/.j/tasks/abc/requirements.md"
		reqBody  = "req body"
		planPath = "/tmp/.j/tasks/abc/plan.md"
		planBody = "plan body"
	)
	got := BuildVerifierResume(reqPath, reqBody, planPath, planBody, "")
	if got == "" {
		t.Fatal("BuildVerifierResume returned empty string")
	}
	lower := strings.ToLower(got)
	for _, marker := range []string{"previous", "check", "continue"} {
		if !strings.Contains(lower, marker) {
			t.Fatalf("resume prompt missing marker %q: %q", marker, got)
		}
	}
	for _, want := range []string{reqPath, reqBody, planPath, planBody} {
		if !strings.Contains(got, want) {
			t.Fatalf("resume prompt missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, strings.TrimSpace(verifier.Instruction)) {
		t.Fatalf("resume prompt should NOT include verifier.Instruction: %q", got)
	}
	for _, banned := range []string{"Save", "Then exit."} {
		if strings.Contains(got, banned) {
			t.Fatalf("resume prompt should not include %q: %q", banned, got)
		}
	}
	if got == BuildVerifier(reqPath, reqBody, planPath, planBody, "vp.md", "vf.md", "", nil) {
		t.Fatal("resume prompt should differ from BuildVerifier output")
	}
	if strings.Contains(strings.ToLower(got), "git worktree") {
		t.Fatalf("empty worktree should not emit worktree line: %q", got)
	}
}

// TestBuildVerifierResume_WithWorktree pins the worktree-direction
// suffix on the resume path, mirroring TestBuildVerifier_WithWorktree.
func TestBuildVerifierResume_WithWorktree(t *testing.T) {
	got := BuildVerifierResume("r.md", "r", "p.md", "p", "j-my-task")
	if !strings.Contains(got, "j-my-task") {
		t.Fatalf("resume worktree prompt missing worktree name: %q", got)
	}
	if !strings.Contains(got, "git worktree list") {
		t.Fatalf("resume worktree prompt missing `git worktree list` hint: %q", got)
	}
}

func TestBuildVerifierFix(t *testing.T) {
	const (
		planPath     = "/tmp/.j/tasks/abc/plan.md"
		planBody     = "plan body"
		findingsPath = "/tmp/.j/tasks/abc/verifier_findings.md"
		findingsBody = "- missing test\nVERDICT: FAIL"
	)
	got := BuildVerifierFix(planPath, planBody, findingsPath, findingsBody, "")
	if got == "" {
		t.Fatal("BuildVerifierFix returned empty string")
	}
	for _, want := range []string{planPath, planBody, findingsPath, findingsBody, "VERDICT: FAIL"} {
		if !strings.Contains(got, want) {
			t.Fatalf("fix prompt missing %q: %q", want, got)
		}
	}
	lower := strings.ToLower(got)
	for _, marker := range []string{"resuming", "findings", "address"} {
		if !strings.Contains(lower, marker) {
			t.Fatalf("fix prompt missing marker %q: %q", marker, got)
		}
	}
	if strings.Contains(got, "Re-plan") || strings.Contains(got, "re-plan") {
		// The prompt body uses "Do not re-plan from scratch."
		// which contains "re-plan"; the marker check above
		// only forbids the literal phrasing "re-plan from scratch"
		// when it would invite a fresh plan.
		if !strings.Contains(got, "Do not re-plan") {
			t.Fatalf("fix prompt should explicitly forbid re-planning: %q", got)
		}
	}
	if strings.Contains(strings.ToLower(got), "git worktree") {
		t.Fatalf("empty worktree should not emit worktree line: %q", got)
	}
}

// TestBuildVerifierFix_WithWorktree pins the worktree-direction suffix
// on the fix-findings coder prompt; BuildVerifierFix shares
// appendWorktreeLine with BuildCoder so the hint mentions
// `git worktree add`, not `git worktree list`.
func TestBuildVerifierFix_WithWorktree(t *testing.T) {
	got := BuildVerifierFix("p.md", "p", "vf.md", "body", "j-my-task")
	if !strings.Contains(got, "j-my-task") {
		t.Fatalf("fix worktree prompt missing worktree name: %q", got)
	}
	if !strings.Contains(got, "git worktree add") {
		t.Fatalf("fix worktree prompt missing `git worktree add` hint: %q", got)
	}
}
