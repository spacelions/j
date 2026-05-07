package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksReadRequirements_EmptyStore pins the acceptance bullet:
// `j tasks read requirements` (no `--from-task`) on an empty store
// short-circuits BEFORE the picker, prints `J: no tasks`, and
// exits 0.
func TestTasksReadRequirements_EmptyStore(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(
		tasks.New(), "read", "requirements",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "J: no tasks") {
		t.Fatalf("stdout = %q, want substring `J: no tasks`",
			stdout)
	}
}

// TestTasksReadPlan_EmptyStore pins the same contract for `read plan`.
func TestTasksReadPlan_EmptyStore(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(
		tasks.New(), "read", "plan",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "J: no tasks") {
		t.Fatalf("stdout = %q, want substring `J: no tasks`",
			stdout)
	}
}
