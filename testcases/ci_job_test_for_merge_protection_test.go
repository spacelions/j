package testcases_test

import (
	"regexp"
	"testing"
)

// TestCIJob_Test_IsRequiredCheckName documents acceptance criteria
// #4 and #5: the CI workflow's job named "test" is the status check
// name GitHub branch protection must require before merging into
// main. The job name must be "test" so that the required status
// check is deterministic and does not depend on removed Claude
// workflows. This test does not verify GitHub repository settings —
// only that the name is well-known and exists.
func TestCIJob_Test_IsRequiredCheckName(t *testing.T) {
	body := readRepoFile(t, ".github", "workflows", "ci.yml")

	jobRe := regexp.MustCompile(`(?m)^jobs:\s*$\n(?:^\s+#.*$\n)*^\s+test:\s*$`)
	if !jobRe.MatchString(body) {
		t.Fatalf("ci.yml: missing `test` job directly under `jobs:` " +
			"— the required status check name for merge " +
			"protection must be \"test\"")
	}
}
