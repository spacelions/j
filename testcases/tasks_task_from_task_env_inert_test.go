package testcases_test

import (
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksTaskFromTask_EnvIsInert pins SPA-57 AC4: the
// `TASKS_TASK_FROM_TASK` env var binding has been removed. With the
// env var set to a real task id, `j tasks show` (no flag) on a
// non-empty store must still drive the picker (or short-circuit on
// empty store), NOT route through the deleted binding to render the
// task. We assert this black-box by setting the env var on an empty
// store: if the env var were still wired anywhere, the unknown-id
// branch would print `J: no task` for a ghost id; with no binding,
// the empty-store branch prints `J: no tasks` instead.
func TestTasksTaskFromTask_EnvIsInert(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	testutil.Init(t)
	t.Setenv("TASKS_TASK_FROM_TASK", "ghost-id")

	stdout, _, err := testutil.RunCobra(t, tasks.New(), "show")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "J: no tasks") {
		t.Fatalf(
			"`TASKS_TASK_FROM_TASK` should be inert; expected "+
				"empty-store branch (`J: no tasks`); got %q",
			stdout,
		)
	}
	if strings.Contains(stdout, "J: no task\n") ||
		strings.HasSuffix(strings.TrimRight(stdout, "\n"),
			"J: no task") {
		t.Fatalf(
			"`TASKS_TASK_FROM_TASK` leaked into another viper "+
				"key and routed to the unknown-id branch: %q",
			stdout,
		)
	}
}
