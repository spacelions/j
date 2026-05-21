package testcases_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBranchCoverage_Make_SkipsWhenNoProductionFilesTouched asserts
// that `make branch-coverage` exits successfully and prints a "no
// production packages touched" notice when the touched file list
// contains only tests, fixtures, configuration, and documentation.
func TestBranchCoverage_Make_SkipsWhenNoProductionFilesTouched(
	t *testing.T,
) {
	t.Parallel()

	files := strings.Join([]string{
		"testcases/foo_test.go",
		"internal/cli/picker/picker_test.go",
		"internal/testutil/seed.go",
		"README.md",
		"AGENTS.md",
		".github/workflows/branch-coverage.yml",
		"Makefile",
		"go.mod",
	}, "\n")

	cmd := exec.CommandContext(t.Context(),
		"make", "branch-coverage")
	cmd.Dir = repoPath(t)
	cmd.Env = append(cmd.Environ(),
		"BRANCH_COVERAGE_FILES="+files,
	)
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err,
		"make branch-coverage failed:\n%s", out)

	output := string(out)
	assert.Contains(t, output,
		"branch coverage: no production packages touched",
		"expected skip notice; got:\n%s", output)
	assert.NotContains(t, output, "Branch coverage:",
		"gobco must not run when no production files touched")
}
