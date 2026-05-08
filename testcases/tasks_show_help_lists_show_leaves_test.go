package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksShowHelp_ListsShowLeaves pins the SPA-57 acceptance bullet:
// `j tasks show --help` continues to list the four surviving leaves
// (`requirements`, `plan`, `clarification`, `findings`) under
// "Available Commands:".
func TestTasksShowHelp_ListsShowLeaves(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(
		tasks.New(), "show", "--help",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	cmds := availableCommandsBlock(stdout)
	if cmds == "" {
		t.Fatalf("missing `Available Commands:` block in: %q",
			stdout)
	}
	want := []string{
		"requirements", "plan", "clarification", "findings",
	}
	for _, leaf := range want {
		found := false
		for _, line := range strings.Split(cmds, "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] == leaf {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf(
				"leaf %q missing from `j tasks show --help`;"+
					" commands=%q",
				leaf, cmds,
			)
		}
	}
}
