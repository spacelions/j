package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksLogs_UnknownID pins the acceptance bullet:
// `j tasks logs --from-task <unknown-id>` short-circuits to the
// no-task branch, prints `J: no task`, and exits 0 with no `tail`
// subprocess.
func TestTasksLogs_UnknownID(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(
		tasks.New(), "logs", "--from-task", "ghost-id",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "J: no task") {
		t.Fatalf("stdout = %q, want substring `J: no task`",
			stdout)
	}
}
