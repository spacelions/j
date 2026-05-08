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

// TestContracts_WorkerOverride_ClarificationPath pins AC#1.2: even
// when worker.md is replaced with a body that does not mention
// `clarification.md`, the composed worker prompt (fresh, resume, and
// fix-findings flavours) still names the per-task absolute
// clarification path and carries the "If you need clarification"
// escape hatch. The contract lives in the always-injected suffix so a
// custom worker body cannot silently drop it.
func TestContracts_WorkerOverride_ClarificationPath(t *testing.T) {
	freshInit(t)

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	override := filepath.Join(dir, "worker_stub.md")
	if err := os.WriteFile(
		override, []byte("You are a worker.\n"), 0o644,
	); err != nil {
		t.Fatalf("write override: %v", err)
	}
	if _, _, err := testutil.RunCobra(
		settings.New(), "set", "worker.prompt="+override,
	); err != nil {
		t.Fatalf("set worker.prompt: %v", err)
	}

	const (
		plan     = "/abs/.j/tasks/T1/plan.md"
		findings = "/abs/.j/tasks/T1/verifier_findings.md"
		clarify  = "/abs/.j/tasks/T1/clarification.md"
	)
	cases := map[string]string{
		"fresh":  prompts.BuildWorker(plan, "", nil, clarify),
		"resume": prompts.BuildWorkerResume(plan, "", nil, clarify),
		"fix":    prompts.BuildVerifierFix(plan, findings, "", clarify),
	}
	for name, p := range cases {
		if !strings.Contains(p, clarify) {
			t.Fatalf("%s worker prompt missing per-task clarification path %q: %q",
				name, clarify, p)
		}
		if !strings.Contains(p, "If you need clarification") {
			t.Fatalf("%s worker prompt missing escape-hatch line: %q",
				name, p)
		}
	}
}
