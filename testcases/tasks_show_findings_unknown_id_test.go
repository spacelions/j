package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksShowFindings_UnknownID pins acceptance criterion #4:
// `j tasks show findings --from-task <unknown-id>` short-circuits
// to the no-task branch, prints `J: no task`, and exits 0.
func TestTasksShowFindings_UnknownID(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(t,
		tasks.New(), "show", "findings",
		"--from-task", "ghost-id",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "J: no task") {
		t.Fatalf("stdout = %q, want substring `J: no task`",
			stdout)
	}
}
