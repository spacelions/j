package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksShowHelp_ListsRequirementsAndPlan pins the acceptance
// bullet: `j tasks show --help` lists `requirements` and `plan`.
func TestTasksShowHelp_ListsRequirementsAndPlan(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(
		tasks.New(), "show", "--help",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"requirements", "plan"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("`j tasks show --help` missing %q: %q",
				want, stdout)
		}
	}
}
