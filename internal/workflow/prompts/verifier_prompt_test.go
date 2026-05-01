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
	got := BuildVerifier(reqPath, reqBody, planPath, planBody, verifierPlanPath, findingsPath)

	if !strings.Contains(got, strings.TrimSpace(verifier.Instruction)) {
		t.Fatalf("prompt missing verifier.Instruction: %q", got)
	}
	for _, want := range []string{reqPath, reqBody, planPath, planBody, verifierPlanPath, findingsPath} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q: %q", want, got)
		}
	}
	for _, want := range []string{"VERDICT: PASS", "VERDICT: FAIL", "Save", "Then exit."} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q: %q", want, got)
		}
	}
}

// TestBuildVerifier_TrimsLeadingWhitespace pins the same trim
// invariant exercised by the planner/coder prompts: the embedded
// verifier.Instruction must not bleed leading whitespace into the
// composed prompt.
func TestBuildVerifier_TrimsLeadingWhitespace(t *testing.T) {
	got := BuildVerifier("r.md", "r", "p.md", "p", "vp.md", "vf.md")
	if strings.HasPrefix(got, "\n") || strings.HasPrefix(got, " ") {
		t.Fatalf("prompt should not start with whitespace: %q", got[:10])
	}
}

func TestBuildVerifierResume(t *testing.T) {
	const (
		reqPath  = "/tmp/.j/tasks/abc/requirements.md"
		reqBody  = "req body"
		planPath = "/tmp/.j/tasks/abc/plan.md"
		planBody = "plan body"
	)
	got := BuildVerifierResume(reqPath, reqBody, planPath, planBody)
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
	if got == BuildVerifier(reqPath, reqBody, planPath, planBody, "vp.md", "vf.md") {
		t.Fatal("resume prompt should differ from BuildVerifier output")
	}
}

func TestBuildVerifierFix(t *testing.T) {
	const (
		planPath     = "/tmp/.j/tasks/abc/plan.md"
		planBody     = "plan body"
		findingsPath = "/tmp/.j/tasks/abc/verifier_findings.md"
		findingsBody = "- missing test\nVERDICT: FAIL"
	)
	got := BuildVerifierFix(planPath, planBody, findingsPath, findingsBody)
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
}
