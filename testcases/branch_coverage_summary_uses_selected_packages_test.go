package testcases_test

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBranchCoverage_Summary_UsesOnlySelectedPackages is a verifier
// acceptance test for AC1 + AC3: when a change touches production Go
// files in two specific packages, `make branch-coverage` must run
// gobco only for those packages and the final aggregated summary line
// must reflect counts from exactly those packages (no broader set).
func TestBranchCoverage_Summary_UsesOnlySelectedPackages(
	t *testing.T,
) {
	t.Parallel()

	// Two distinct production packages touched. We use small, stable
	// internal packages so the test is fast and deterministic enough
	// for an e2e check.
	files := strings.Join([]string{
		"internal/cli/version/cmd.go",
		"internal/cli/picker/picker.go",
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

	// gobco must run for each selected package (Go test runner
	// prints `ok  <pkg>` lines for each tested package).
	assert.Contains(t, output,
		"github.com/spacelions/j/internal/cli/version",
		"version package must be analyzed; got:\n%s", output)
	assert.Contains(t, output,
		"github.com/spacelions/j/internal/cli/picker",
		"picker package must be analyzed; got:\n%s", output)

	// gobco must NOT run for unrelated packages.
	for _, untouched := range []string{
		"github.com/spacelions/j/internal/cli/initcmd",
		"github.com/spacelions/j/internal/cli/run",
		"github.com/spacelions/j/internal/cli/tasks",
		"github.com/spacelions/j/internal/agents/planner",
	} {
		assert.NotContainsf(t, output, untouched,
			"untouched package %s must not be analyzed; "+
				"got:\n%s", untouched, output)
	}

	// Final aggregated summary line must be present and parseable.
	re := regexp.MustCompile(
		`(?m)^branch coverage: \d+\.\d+% \(\d+/\d+\)\s*$`)
	assert.Truef(t, re.MatchString(output),
		"missing aggregated summary line; got:\n%s", output)

	// The skip notice must NOT appear: we did supply production
	// files, so analysis must run.
	assert.NotContains(t, output,
		"no production packages touched",
		"skip notice must not fire when prod files supplied; "+
			"got:\n%s", output)
}
