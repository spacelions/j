package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksReadRequirements_UnknownID pins the acceptance bullet:
// `j tasks read requirements --from-task <unknown-id>` short-circuits
// to the no-task branch, prints `J: no task`, and exits 0.
func TestTasksReadRequirements_UnknownID(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(
		tasks.New(), "read", "requirements",
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

// TestTasksReadPlan_UnknownID pins the same contract for the
// `read plan` leaf.
func TestTasksReadPlan_UnknownID(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(
		tasks.New(), "read", "plan",
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
