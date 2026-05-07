package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksHelp_ListsShowLogsTask pins the acceptance bullet:
// `j tasks --help` lists `show`, `logs`, and `task` as subcommands.
func TestTasksHelp_ListsShowLogsTask(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(tasks.New(), "--help")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"show", "logs", "task"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("`j tasks --help` missing %q: %q",
				want, stdout)
		}
	}
}
