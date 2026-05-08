package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksShowRequirements_EmptyStore pins the acceptance bullet:
// `j tasks show requirements` (no `--from-task`) on an empty store
// short-circuits BEFORE the picker, prints `J: no tasks`, and
// exits 0.
func TestTasksShowRequirements_EmptyStore(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(t,
		tasks.New(), "show", "requirements",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "J: no tasks") {
		t.Fatalf("stdout = %q, want substring `J: no tasks`",
			stdout)
	}
}

// TestTasksShowPlan_EmptyStore pins the same contract for `show plan`.
func TestTasksShowPlan_EmptyStore(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(t,
		tasks.New(), "show", "plan",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "J: no tasks") {
		t.Fatalf("stdout = %q, want substring `J: no tasks`",
			stdout)
	}
}
