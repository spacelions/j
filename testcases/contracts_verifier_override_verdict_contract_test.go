package testcases_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/prompts"
	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/testutil"
)

// TestContracts_VerifierOverride_VerdictContract pins AC#1.3: even
// when verifier.md is replaced with a body that mentions none of
// `verifier_findings.md`, `VERDICT: PASS`, or `VERDICT: FAIL`, the
// composed verifier prompt (fresh and resume) still names the
// per-task findings path, the per-task clarification path, and
// carries the "last non-empty line" + VERDICT contract. The contract
// lives in the always-injected verifier_request.md suffix so a custom
// verifier body cannot silently drop it.
func TestContracts_VerifierOverride_VerdictContract(t *testing.T) {
	freshInit(t)

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	override := filepath.Join(dir, "verifier_stub.md")
	if err := os.WriteFile(
		override, []byte("You are a verifier.\n"), 0o644,
	); err != nil {
		t.Fatalf("write override: %v", err)
	}
	if _, _, err := testutil.RunCobra(
		settings.New(), "set", "verifier.prompt="+override,
	); err != nil {
		t.Fatalf("set verifier.prompt: %v", err)
	}

	const (
		req      = "/abs/.j/tasks/T1/requirements.md"
		plan     = "/abs/.j/tasks/T1/plan.md"
		vplan    = "/abs/.j/tasks/T1/verifier_plan.md"
		findings = "/abs/.j/tasks/T1/verifier_findings.md"
		clarify  = "/abs/.j/tasks/T1/clarification.md"
	)
	got := prompts.BuildVerifier(
		req, plan, vplan, findings, "", nil, clarify,
	)
	for _, want := range []string{
		req, plan, findings, clarify,
		"VERDICT: PASS", "VERDICT: FAIL", "last non-empty line",
		"If you need clarification",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("fresh verifier prompt missing %q despite stub override: %q",
				want, got)
		}
	}

	resume := prompts.BuildVerifierResume(req, plan, "", nil, clarify)
	for _, want := range []string{
		req, plan, clarify, "If you need clarification",
	} {
		if !strings.Contains(resume, want) {
			t.Fatalf("verifier-resume prompt missing %q despite stub override: %q",
				want, resume)
		}
	}
}
