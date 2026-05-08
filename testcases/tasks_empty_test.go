package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksEmpty_PrintsNoTasksLine pins `j tasks` against a freshly
// initialised project: the bbolt log carries zero rows, the command
// short-circuits to the empty-message branch, and the only thing on
// stdout is `J: no tasks\n`.
//
// Replaces testcases/tasks-empty.md.
func TestTasksEmpty_PrintsNoTasksLine(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, stderr, err := testutil.RunCobra(t, tasks.New())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stdout != "J: no tasks\n" {
		t.Fatalf("stdout = %q, want %q", stdout, "J: no tasks\n")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}
