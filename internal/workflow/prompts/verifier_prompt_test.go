package prompts

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/workflow/instructions"
)

func TestBuildVerifier(t *testing.T) {
	const (
		reqPath          = "/tmp/.j/tasks/abc/requirements.md"
		planPath         = "/tmp/.j/tasks/abc/plan.md"
		verifierPlanPath = "/tmp/.j/tasks/abc/verifier_plan.md"
		findingsPath     = "/tmp/.j/tasks/abc/verifier_findings.md"
	)
	got := BuildVerifier(reqPath, planPath, verifierPlanPath, findingsPath, "", nil)

	if !strings.Contains(got, strings.TrimSpace(instructions.Verifier)) {
		t.Fatalf("prompt missing instructions.Verifier: %q", got)
	}
	for _, want := range []string{reqPath, planPath, findingsPath} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q: %q", want, got)
		}
	}
	if !strings.Contains(got, "Read the requirements at") {
		t.Fatalf("prompt missing read-the-requirements directive: %q", got)
	}
	if strings.Contains(got, verifierPlanPath) {
		t.Fatalf("prompt should not reference verifier_plan.md: %q", got)
	}
	if strings.Contains(got, "Requirements (from") || strings.Contains(got, "Plan (from") {
		t.Fatalf("prompt should not embed requirement/plan bodies: %q", got)
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

// TestBuildVerifier_WithMustRead pins the bulleted must-read block on
// the verifier prompt: appears once, preserves case, and sits between
// the verifier instruction and the read-the-requirements directive.
func TestBuildVerifier_WithMustRead(t *testing.T) {
	got := BuildVerifier("r.md", "p.md", "vp.md", "vf.md", "", []string{"AGENTS.md", "CLAUDE.md"})
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
	if strings.Index(got, header) > strings.Index(got, "Read the requirements at") {
		t.Fatalf("must-read block must precede read-the-requirements line: %q", got)
	}
}

// TestBuildVerifier_TrimsLeadingWhitespace pins the same trim
// invariant exercised by the planner/worker prompts: the embedded
// instructions.Verifier must not bleed leading whitespace into the
// composed prompt.
func TestBuildVerifier_TrimsLeadingWhitespace(t *testing.T) {
	got := BuildVerifier("r.md", "p.md", "vp.md", "vf.md", "", nil)
	if strings.HasPrefix(got, "\n") || strings.HasPrefix(got, " ") {
		t.Fatalf("prompt should not start with whitespace: %q", got[:10])
	}
}

// TestBuildVerifier_WithWorktree pins the worktree-direction suffix
// on the first-run verifier prompt: the trailing line names the
// worktree verbatim and mentions `git worktree list` so the verifier
// knows how to resolve the absolute path.
func TestBuildVerifier_WithWorktree(t *testing.T) {
	got := BuildVerifier("r.md", "p.md", "vp.md", "vf.md", "j-my-task", nil)
	if !strings.Contains(got, "j-my-task") {
		t.Fatalf("worktree prompt missing worktree name: %q", got)
	}
	if !strings.Contains(got, "git worktree list") {
		t.Fatalf("worktree prompt missing `git worktree list` hint: %q", got)
	}
}

// TestBuildVerifierResume pins the resume-only verifier prompt: the
// rendered text must be non-empty, embed the instructions.Verifier
// body (whose opening line "You are the verifier …" doubles as the
// role preamble — so no duplicate sentence), mention the
// "previous / check / continue" semantics, cite the requirement and
// plan paths without inlining their bodies, and differ from
// BuildVerifier.
func TestBuildVerifierResume(t *testing.T) {
	const (
		reqPath  = "/tmp/.j/tasks/abc/requirements.md"
		planPath = "/tmp/.j/tasks/abc/plan.md"
	)
	got := BuildVerifierResume(reqPath, planPath, "")
	if got == "" {
		t.Fatal("BuildVerifierResume returned empty string")
	}
	if !strings.Contains(got, strings.TrimSpace(instructions.Verifier)) {
		t.Fatalf("resume prompt missing instructions.Verifier: %q", got)
	}
	const preamble = "You are the verifier in a planner/worker/verifier workflow."
	if strings.Count(got, preamble) != 1 {
		t.Fatalf("resume prompt should contain the role preamble exactly once (no duplicate): %q", got)
	}
	lower := strings.ToLower(got)
	for _, marker := range []string{"previous", "check", "continue"} {
		if !strings.Contains(lower, marker) {
			t.Fatalf("resume prompt missing marker %q: %q", marker, got)
		}
	}
	for _, want := range []string{reqPath, planPath} {
		if !strings.Contains(got, want) {
			t.Fatalf("resume prompt missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "Requirements (from") || strings.Contains(got, "Plan (from") {
		t.Fatalf("resume prompt should not embed requirement/plan bodies: %q", got)
	}
	for _, banned := range []string{"Save your final findings", "Then exit."} {
		if strings.Contains(got, banned) {
			t.Fatalf("resume prompt should not include %q: %q", banned, got)
		}
	}
	if got == BuildVerifier(reqPath, planPath, "vp.md", "vf.md", "", nil) {
		t.Fatal("resume prompt should differ from BuildVerifier output")
	}
	if strings.Contains(strings.ToLower(got), "git worktree") {
		t.Fatalf("empty worktree should not emit worktree line: %q", got)
	}
}

// TestBuildVerifierResume_WithWorktree pins the worktree-direction
// suffix on the resume path, mirroring TestBuildVerifier_WithWorktree.
func TestBuildVerifierResume_WithWorktree(t *testing.T) {
	got := BuildVerifierResume("r.md", "p.md", "j-my-task")
	if !strings.Contains(got, "j-my-task") {
		t.Fatalf("resume worktree prompt missing worktree name: %q", got)
	}
	if !strings.Contains(got, "git worktree list") {
		t.Fatalf("resume worktree prompt missing `git worktree list` hint: %q", got)
	}
}

// TestBuildVerifierFix pins the fix-loop worker prompt: the rendered
// text must be non-empty, embed the instructions.Worker body (whose
// opening "You are the worker …" doubles as the role preamble — so
// no duplicate sentence), reference the plan path for context only
// and the findings path as the action list, and explicitly forbid
// re-planning.
func TestBuildVerifierFix(t *testing.T) {
	const (
		planPath     = "/tmp/.j/tasks/abc/plan.md"
		findingsPath = "/tmp/.j/tasks/abc/verifier_findings.md"
	)
	got := BuildVerifierFix(planPath, findingsPath, "")
	if got == "" {
		t.Fatal("BuildVerifierFix returned empty string")
	}
	if !strings.Contains(got, strings.TrimSpace(instructions.Worker)) {
		t.Fatalf("fix prompt missing instructions.Worker: %q", got)
	}
	const preamble = "You are the worker in a planner/worker/verifier workflow."
	if strings.Count(got, preamble) != 1 {
		t.Fatalf("fix prompt should contain the role preamble exactly once (no duplicate): %q", got)
	}
	for _, want := range []string{planPath, findingsPath, "Address every item"} {
		if !strings.Contains(got, want) {
			t.Fatalf("fix prompt missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "Verifier findings (from") || strings.Contains(got, "Plan (from") {
		t.Fatalf("fix prompt should not embed plan/findings bodies: %q", got)
	}
	lower := strings.ToLower(got)
	for _, marker := range []string{"resuming", "findings", "address"} {
		if !strings.Contains(lower, marker) {
			t.Fatalf("fix prompt missing marker %q: %q", marker, got)
		}
	}
	if !strings.Contains(got, "Do not re-plan") {
		t.Fatalf("fix prompt should explicitly forbid re-planning: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "git worktree") {
		t.Fatalf("empty worktree should not emit worktree line: %q", got)
	}
}

// TestBuildVerifierFix_WithWorktree pins the worktree-direction suffix
// on the fix-findings worker prompt; BuildVerifierFix shares
// appendWorktreeLine with BuildWorker so the hint mentions
// `git worktree add`, not `git worktree list`.
func TestBuildVerifierFix_WithWorktree(t *testing.T) {
	got := BuildVerifierFix("p.md", "vf.md", "j-my-task")
	if !strings.Contains(got, "j-my-task") {
		t.Fatalf("fix worktree prompt missing worktree name: %q", got)
	}
	if !strings.Contains(got, "git worktree add") {
		t.Fatalf("fix worktree prompt missing `git worktree add` hint: %q", got)
	}
}
