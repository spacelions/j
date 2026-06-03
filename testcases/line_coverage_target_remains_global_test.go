package testcases_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLineCoverage_Target_RemainsGlobalAndUnchanged is a verifier
// acceptance test for AC5: existing line coverage behavior is
// unchanged by the branch-coverage scoping work. Specifically, the
// `line-coverage` Makefile target must still:
//   - derive its package set globally from `go list ./internal/...`,
//   - exclude only the `internal/testutil` helper package,
//   - retain its allowlist-driven failure path (exit 1 on missing
//     coverage),
//   - NOT depend on the new `.hooks/branch-coverage-pkgs` script.
func TestLineCoverage_Target_RemainsGlobalAndUnchanged(
	t *testing.T,
) {
	t.Parallel()

	body := readRepoFile(t, "Makefile")
	section := makefileTargetSection(t, body, "line-coverage")

	assert.Contains(t, section, "go list ./internal/...",
		"line-coverage must enumerate all internal packages "+
			"globally; got:\n%s", section)
	assert.Contains(t, section, "internal/testutil",
		"line-coverage must keep its testutil exclusion; "+
			"got:\n%s", section)
	assert.Contains(t, section, "exit 1",
		"line-coverage must keep its allowlist-driven "+
			"failure path; got:\n%s", section)
	assert.NotContains(t, section, "branch-coverage-pkgs",
		"line-coverage must not depend on the branch-"+
			"coverage scoping hook; got:\n%s", section)
	assert.NotContains(t, section, "BRANCH_COVERAGE_FILES",
		"line-coverage must not read the BRANCH_COVERAGE_"+
			"FILES override; got:\n%s", section)

	// The `coverage` alias must still point at line-coverage so
	// that `make coverage` keeps its original meaning.
	alias := makefileTargetSection(t, body, "coverage")
	first := strings.SplitN(alias, "\n", 2)[0]
	assert.Equal(t, "coverage: line-coverage", first,
		"coverage alias changed; got: %q", first)
}
