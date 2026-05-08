package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/instructions"
)

// TestRoleBodies_DropContractMentions pins the AC: the embedded role
// bodies (planner.md / worker.md / verifier.md) — the ones a user
// can override — must no longer mention canonical filenames or the
// VERDICT contract. Those contracts now live exclusively in the
// always-injected per-phase tails so a custom override cannot drop
// them. This is the regression guard against re-introducing
// contract mentions back into a user-overridable surface.
func TestRoleBodies_DropContractMentions(t *testing.T) {
	cases := []struct {
		name string
		body string
		// banned substrings must NOT appear anywhere in the body.
		banned []string
	}{
		{
			name: "planner.md",
			body: instructions.Planner,
			banned: []string{
				"clarification.md",
				"requirements.md",
				"plan.md",
				"VERDICT",
				"verifier_findings.md",
			},
		},
		{
			name: "worker.md",
			body: instructions.Worker,
			banned: []string{
				"clarification.md",
				"verifier_findings.md",
				"VERDICT",
			},
		},
		{
			name: "verifier.md",
			body: instructions.Verifier,
			banned: []string{
				"clarification.md",
				"verifier_findings.md",
				// The verifier role body must not pin the
				// VERDICT contract — it lives in
				// verifier_request.md so a custom verifier.md
				// cannot drop it.
				"VERDICT: PASS",
				"VERDICT: FAIL",
			},
		},
	}
	for _, c := range cases {
		for _, b := range c.banned {
			if strings.Contains(c.body, b) {
				t.Fatalf("%s still mentions %q (must move to "+
					"the always-injected per-phase tail "+
					"so an override cannot drop it):\n%s",
					c.name, b, c.body)
			}
		}
	}
}
