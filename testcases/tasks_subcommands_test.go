package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksDiscard_UnknownID pins `j tasks discard --id ghost-id --yes`
// against an empty store: the command short-circuits to the "no task"
// branch, prints `J: no task`, and exits 0 (per `j tasks discard
// --help`).
//
// Replaces testcases/tasks-discard-unknown-id.md.
func TestTasksDiscard_UnknownID(t *testing.T) {
	freshInit(t)

	stdout, _, err := testutil.RunCobra(tasks.New(),
		"discard", "--id", "ghost-id", "--yes",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "J: no task") {
		t.Fatalf("stdout = %q, want `J: no task`", stdout)
	}
}

// TestTasksEnter_EmptyStore pins `j tasks enter` (no `--id`, no
// `--print`) on a freshly-initialised, empty task store: the
// empty-store branch short-circuits BEFORE the huh picker, prints
// `J: no tasks`, and exits 0.
//
// Replaces testcases/tasks-enter-empty-store.md.
func TestTasksEnter_EmptyStore(t *testing.T) {
	freshInit(t)

	stdout, _, err := testutil.RunCobra(tasks.New(), "enter")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "J: no tasks") {
		t.Fatalf("stdout = %q, want `J: no tasks`", stdout)
	}
}
