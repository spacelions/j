package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksTask_UnknownCommand pins the SPA-57 acceptance bullet:
// `j tasks task` is no longer a recognised subcommand. Cobra reports
// the standard "unknown command" diagnostic and Execute returns a
// non-nil error so the process exits non-zero.
func TestTasksTask_UnknownCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, stderr, err := testutil.RunCobra(tasks.New(), "task")
	if err == nil {
		t.Fatalf(
			"`j tasks task` returned nil; want unknown-command "+
				"error. stdout=%q stderr=%q",
			stdout, stderr,
		)
	}
	combined := stdout + stderr + err.Error()
	if !strings.Contains(combined, "unknown command") {
		t.Fatalf(
			"expected `unknown command` diagnostic; got "+
				"err=%v stdout=%q stderr=%q",
			err, stdout, stderr,
		)
	}
	if !strings.Contains(combined, "task") {
		t.Fatalf(
			"diagnostic should name the offending arg `task`; "+
				"got err=%v stdout=%q stderr=%q",
			err, stdout, stderr,
		)
	}
}
