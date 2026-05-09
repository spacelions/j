package testcases_test

import (
	"regexp"
	"testing"
)

// TestAGENTSMd_NoBranchCoveragePercentage asserts that AGENTS.md does
// not require contributors to maintain any specific branch coverage
// percentage, since branch coverage is now informational only.
func TestAGENTSMd_NoBranchCoveragePercentage(t *testing.T) {
	body := readRepoFile(t, "AGENTS.md")

	pctRe := regexp.MustCompile(
		`(?i)branch\s+coverage[^.]*\d+%`,
	)
	if pctRe.MatchString(body) {
		t.Fatalf("AGENTS.md must not specify a branch coverage " +
			"percentage target")
	}
}
