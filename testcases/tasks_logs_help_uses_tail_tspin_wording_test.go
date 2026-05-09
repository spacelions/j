package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksLogs_HelpUsesTailTspinWording pins the acceptance bullet:
// `j tasks logs --help` advertises the `tail -f` follower (with the
// optional `tspin` pipe) and drops every reference to `bat` or `cat`.
func TestTasksLogs_HelpUsesTailTspinWording(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(t,
		tasks.New(), "logs", "--help",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "tail -f") ||
		!strings.Contains(stdout, "tspin") {
		t.Fatalf(
			"help missing tail -f / tspin wording: %q", stdout,
		)
	}
	for _, banned := range []string{"bat", "cat"} {
		if strings.Contains(stdout, banned) {
			t.Fatalf(
				"help still references %q: %q", banned, stdout,
			)
		}
	}
}
