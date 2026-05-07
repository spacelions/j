package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksLogs_HelpUsesBatCatWording pins the acceptance bullet:
// `j tasks logs --help` advertises the bat/cat renderer chain and
// drops every reference to `tail -f` or "Tail" wording.
func TestTasksLogs_HelpUsesBatCatWording(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(
		tasks.New(), "logs", "--help",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "bat") ||
		!strings.Contains(stdout, "cat") {
		t.Fatalf(
			"help missing bat/cat wording: %q", stdout,
		)
	}
	for _, banned := range []string{"tail -f", "Tail "} {
		if strings.Contains(stdout, banned) {
			t.Fatalf(
				"help still references %q: %q", banned, stdout,
			)
		}
	}
}
