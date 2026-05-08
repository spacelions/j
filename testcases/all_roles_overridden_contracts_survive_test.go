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

// TestAllRolesOverridden_ContractsSurvive pins the strongest AC:
// "Replacing any role prompt with a body that mentions none of
// requirements.md, plan.md, verifier_findings.md, or clarification.md
// still preserves all four contracts end-to-end."
//
// The test seeds three stub override files (one per role) whose
// bodies do NOT mention any canonical filename, the VERDICT
// contract, the PM/QA-tone phrasing, the one-line-summary rule, or
// the clarification escape hatch. After the override is in effect,
// every composed prompt MUST still surface the contracts via the
// always-injected per-phase tails.
func TestAllRolesOverridden_ContractsSurvive(t *testing.T) {
	freshInit(t)

	const (
		stubPlanner  = "You are a planner.\n"
		stubWorker   = "You are a worker.\n"
		stubVerifier = "You are a verifier.\n"
	)
	dir := t.TempDir()
	plannerPath := filepath.Join(dir, "p.md")
	workerPath := filepath.Join(dir, "w.md")
	verifierPath := filepath.Join(dir, "v.md")
	for path, body := range map[string]string{
		plannerPath:  stubPlanner,
		workerPath:   stubWorker,
		verifierPath: stubVerifier,
	} {
		if err := os.WriteFile(
			path, []byte(body), 0o644,
		); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	for _, kv := range []struct {
		bucket, path string
	}{
		{"planner", plannerPath},
		{"worker", workerPath},
		{"verifier", verifierPath},
	} {
		if _, _, err := testutil.RunCobra(t, settings.New(),
			"set", kv.bucket+".prompt="+kv.path,
		); err != nil {
			t.Fatalf("set %s: %v", kv.bucket, err)
		}
	}

	const (
		req      = "/abs/.j/tasks/x/requirements.md"
		plan     = "/abs/.j/tasks/x/plan.md"
		findings = "/abs/.j/tasks/x/verifier_findings.md"
		vplan    = "/abs/.j/tasks/x/verifier_plan.md"
		clarify  = "/abs/.j/tasks/x/clarification.md"
	)
	plannerOut := prompts.AppendPlannerSaveSuffix(
		prompts.BuildPlanner(req, nil), req, plan, clarify,
	)
	plannerResume := prompts.AppendPlannerSaveSuffix(
		prompts.BuildPlannerResume(req, nil), req, plan, clarify,
	)
	workerOut := prompts.BuildWorker(plan, "", nil, clarify)
	workerResume := prompts.BuildWorkerResume(plan, "", nil, clarify)
	verifierOut := prompts.BuildVerifier(
		req, plan, vplan, findings, "", nil, clarify,
	)
	verifierResume := prompts.BuildVerifierResume(
		req, plan, "", nil, clarify,
	)
	fixOut := prompts.BuildVerifierFix(plan, findings, "", clarify)

	// Per-prompt expectations keyed to the contracts each prompt
	// MUST carry even when the role body is a stub.
	cases := []struct {
		name string
		got  string
		want []string
	}{
		{"planner-fresh", plannerOut, []string{
			req, plan, clarify,
			"one-line summary", "PM/QA-style spec",
			"acceptance criteria",
			"belong in plan.md",
			"If you need clarification",
		}},
		{"planner-resume", plannerResume, []string{
			req, plan, clarify,
			"one-line summary", "PM/QA-style spec",
		}},
		{"worker-fresh", workerOut, []string{
			plan, clarify, "If you need clarification",
		}},
		{"worker-resume", workerResume, []string{
			plan, clarify, "If you need clarification",
		}},
		{"verifier-fresh", verifierOut, []string{
			req, plan, findings, clarify,
			"VERDICT: PASS", "VERDICT: FAIL",
			"last non-empty line",
			"If you need clarification",
		}},
		{"verifier-resume", verifierResume, []string{
			req, plan, clarify, "If you need clarification",
		}},
		{"verifier-fix", fixOut, []string{
			plan, findings, clarify, "If you need clarification",
		}},
	}
	for _, c := range cases {
		for _, w := range c.want {
			if !strings.Contains(c.got, w) {
				t.Fatalf("%s prompt missing contract %q "+
					"despite stub override (suffix should "+
					"carry it):\n%s", c.name, w, c.got)
			}
		}
	}

	// Sanity: every stub body actually appears in its role's
	// composed prompts so we know the override path is wired and
	// the assertions above are not silently passing because the
	// embedded body still drives the prompt.
	if !strings.Contains(plannerOut, "You are a planner.") {
		t.Fatalf("planner stub body missing from prompt: %q", plannerOut)
	}
	if !strings.Contains(workerOut, "You are a worker.") {
		t.Fatalf("worker stub body missing from prompt: %q", workerOut)
	}
	if !strings.Contains(verifierOut, "You are a verifier.") {
		t.Fatalf("verifier stub body missing from prompt: %q",
			verifierOut)
	}
}
