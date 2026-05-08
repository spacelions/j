package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksShowHelp_ListsFindingsLeaf pins acceptance criterion #1:
// `j tasks show --help` lists the new `findings` leaf alongside the
// existing `requirements` and `plan` leaves.
func TestTasksShowHelp_ListsFindingsLeaf(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(
		tasks.New(), "show", "--help",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "findings") {
		t.Fatalf("`j tasks show --help` missing `findings`: %q",
			stdout)
	}
}
