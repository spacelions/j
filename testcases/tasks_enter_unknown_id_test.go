package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksEnterUnknownID pins `j tasks enter --id ghost-id` against
// a project with no rows: the command must print the "no task" line
// and exit 0 (per `j tasks enter --help`: "Unknown ids print
// `J: no task` and exit 0").
//
// Replaces testcases/tasks-enter-unknown-id.md.
func TestTasksEnterUnknownID(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(
		tasks.New(), "enter", "--id", "ghost-id",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "J: no task") {
		t.Fatalf("stdout = %q, want substring %q",
			stdout, "J: no task")
	}
}
