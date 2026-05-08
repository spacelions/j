package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// TestContracts_CanonicalFilenamesPinned pins AC#2: the four canonical
// filenames must remain the literal strings the orchestrator and the
// `j tasks` summary derivation depend on. Renaming any of them via
// constant rewrite would silently shift the on-disk contract; this
// test catches that. (User-configurable filenames are explicitly
// out-of-scope for this task; if a future change introduces a setting,
// it must keep these defaults intact.)
func TestContracts_CanonicalFilenamesPinned(t *testing.T) {
	cases := map[string]string{
		"requirements": tasks.RequirementsFileName,
		"plan":         tasks.PlanFileName,
		"verdict":      tasks.VerifierFindingsFileName,
		"clarify":      tasks.ClarificationFileName,
	}
	want := map[string]string{
		"requirements": "requirements.md",
		"plan":         "plan.md",
		"verdict":      "verifier_findings.md",
		"clarify":      "clarification.md",
	}
	for k, got := range cases {
		if got != want[k] {
			t.Fatalf("%s filename = %q, want %q", k, got, want[k])
		}
	}
}
