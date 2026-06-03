package testcases_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBranchCoverage_Workflow_ProvidesChangeRangeContext asserts that
// the dedicated branch-coverage workflow checks out enough git history
// and exports the GitHub event metadata that the branch-coverage make
// target needs to derive the touched-file range.
func TestBranchCoverage_Workflow_ProvidesChangeRangeContext(
	t *testing.T,
) {
	body := readRepoFile(t,
		".github", "workflows", "branch-coverage.yml")

	fetchDepth := regexp.MustCompile(
		`(?m)^\s+fetch-depth:\s*0\s*$`)
	assert.True(t, fetchDepth.MatchString(body),
		"branch-coverage.yml must set fetch-depth: 0 so the "+
			"target can resolve historical commit ranges")

	envVars := []string{
		"EVENT_NAME",
		"BEFORE_SHA",
		"PR_BASE_SHA",
		"PR_HEAD_SHA",
	}
	for _, name := range envVars {
		re := regexp.MustCompile(
			`(?m)^\s+` + regexp.QuoteMeta(name) + `:\s*\$\{\{`)
		assert.Truef(t, re.MatchString(body),
			"branch-coverage.yml must export %s from the "+
				"GitHub event context", name)
	}
}
