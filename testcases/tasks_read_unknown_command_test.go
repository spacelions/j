package testcases_test

import (
	"strings"
	"testing"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksRead_IsUnknownCommand verifies that `j tasks read` is no
// longer a recognised subcommand and returns a cobra "unknown command"
// error.
func TestTasksRead_IsUnknownCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	_, stderr, err := testutil.RunCobra(clitasks.New(), "read")
	if err == nil {
		t.Fatal("expected `j tasks read` to fail as unknown command")
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Fatalf("stderr = %q, want substring `unknown command`",
			stderr)
	}
}
