package testcases_test

import (
	"strings"
	"testing"
)

// TestLefthook_PreCommit_DoesNotRunBranchCoverage asserts that the
// local pre-commit hook configuration never invokes branch coverage,
// keeping local development fast.
func TestLefthook_PreCommit_DoesNotRunBranchCoverage(t *testing.T) {
	body := readRepoFile(t, "lefthook.yml")
	if strings.Contains(body, "branch-coverage") {
		t.Fatalf("lefthook.yml must not reference branch-coverage; " +
			"branch coverage should only run in its dedicated workflow")
	}
}
