package testcases_test

import (
	"strings"
	"testing"
)

// TestContracts_FixFindings_AbsolutePath pins AC#4: when the verify
// loop fires a fix run, the composed prompt must cite the
// task-specific absolute findings path threaded in by the orchestrator
// (not the bare literal "verifier_findings.md") — the cursor backend
// previously rendered the literal which would not resolve from the
// agent's working directory. The claude backend already routes the
// absolute path; this guards both backends through the shared
// BuildVerifierFix builder.
func TestContracts_FixFindings_AbsolutePath(t *testing.T) {
	const (
		plan     = "/abs/.j/tasks/T1/plan.md"
		findings = "/abs/.j/tasks/T1/verifier_findings.md"
		clarify  = "/abs/.j/tasks/T1/clarification.md"
	)
	got := buildVerifierFixPrompt(plan, findings, "", clarify)
	if !strings.Contains(got, findings) {
		t.Fatalf("fix prompt missing absolute findings path %q: %q",
			findings, got)
	}
	if !strings.Contains(got, "Address every item") {
		t.Fatalf("fix prompt missing address-every-item directive: %q", got)
	}
	if !strings.Contains(got, plan) {
		t.Fatalf("fix prompt missing plan path %q: %q", plan, got)
	}
	if !strings.Contains(got, clarify) {
		t.Fatalf("fix prompt missing clarification path %q: %q",
			clarify, got)
	}
	bareLine := "findings at \"verifier_findings.md\""
	if strings.Contains(got, bareLine) {
		t.Fatalf("fix prompt regressed to bare literal filename: %q", got)
	}
}
