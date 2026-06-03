package testcases_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBranchCoveragePkgs_SkipsNonProductionPaths asserts that the
// branch-coverage path filter drops tests, fixtures, configuration,
// documentation, workflow files, the testutil helper package, and any
// non-Go path — leaving only touched production Go packages under
// internal/.
func TestBranchCoveragePkgs_SkipsNonProductionPaths(t *testing.T) {
	t.Parallel()

	files := strings.Join([]string{
		"testcases/foo_test.go",
		"internal/cli/picker/picker_test.go",
		"internal/testutil/seed.go",
		"README.md",
		"AGENTS.md",
		"docs/notes.md",
		".github/workflows/branch-coverage.yml",
		"Makefile",
		"lefthook.yml",
		"go.mod",
		"go.sum",
		"coverage.allowlist",
	}, "\n")

	dirs := runBranchCoveragePkgs(t, files)
	assert.Empty(t, dirs,
		"non-production input must yield no package dirs, got %q",
		dirs)
}

// TestBranchCoveragePkgs_SelectsTouchedProductionPackages asserts that
// the filter maps production Go files to their unique parent package
// directories, deduplicating sibling files and skipping co-located
// tests/testutil entries.
func TestBranchCoveragePkgs_SelectsTouchedProductionPackages(
	t *testing.T,
) {
	t.Parallel()

	files := strings.Join([]string{
		"internal/cli/picker/picker.go",
		"internal/cli/picker/extra.go",
		"internal/cli/picker/picker_test.go",
		"internal/cli/version/version.go",
		"internal/testutil/seed.go",
		"README.md",
	}, "\n")

	dirs := runBranchCoveragePkgs(t, files)
	assert.Equal(t, []string{
		"./internal/cli/picker",
		"./internal/cli/version",
	}, dirs)
}

// TestBranchCoveragePkgs_SkipsCmdAndExternalPaths asserts that the
// filter keeps branch coverage scoped to the internal/ domain and does
// not pull in cmd/ or paths outside the module's branch-coverage set.
func TestBranchCoveragePkgs_SkipsCmdAndExternalPaths(t *testing.T) {
	t.Parallel()

	files := strings.Join([]string{
		"cmd/j/main.go",
		"bin/j",
		"internal/cli/picker/picker.go",
	}, "\n")

	dirs := runBranchCoveragePkgs(t, files)
	assert.Equal(t, []string{"./internal/cli/picker"}, dirs)
}

func runBranchCoveragePkgs(t *testing.T, files string) []string {
	t.Helper()

	script := filepath.Join(
		repoPath(t), ".hooks", "branch-coverage-pkgs",
	)
	cmd := exec.CommandContext(t.Context(), script)
	cmd.Dir = repoPath(t)
	cmd.Env = append(cmd.Environ(),
		"BRANCH_COVERAGE_FILES="+files,
	)
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err,
		".hooks/branch-coverage-pkgs failed:\n%s", out)

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
