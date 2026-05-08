package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksHelp_OmitsTaskSubcommand pins the SPA-57 acceptance bullet:
// `j tasks --help` no longer mentions a `task` subcommand under the
// "Available Commands:" section. The other surviving subcommands the
// requirements call out (`show`, `logs`) must still be listed.
func TestTasksHelp_OmitsTaskSubcommand(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	stdout, _, err := testutil.RunCobra(tasks.New(), "--help")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	cmds := availableCommandsBlock(stdout)
	if cmds == "" {
		t.Fatalf("could not find Available Commands block in: %q",
			stdout)
	}
	for _, line := range strings.Split(cmds, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "task" {
			t.Fatalf(
				"`task` should not appear as a subcommand in "+
					"`j tasks --help`; got line %q",
				line,
			)
		}
	}
	for _, want := range []string{"show", "logs"} {
		found := false
		for _, line := range strings.Split(cmds, "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf(
				"`%s` should still appear as a subcommand "+
					"in `j tasks --help`; commands=%q",
				want, cmds,
			)
		}
	}
}

// availableCommandsBlock returns the body cobra prints under the
// "Available Commands:" heading (up to the next blank line). Returns
// "" when the heading is missing.
func availableCommandsBlock(help string) string {
	const header = "Available Commands:"
	idx := strings.Index(help, header)
	if idx < 0 {
		return ""
	}
	rest := help[idx+len(header):]
	end := strings.Index(rest, "\n\n")
	if end < 0 {
		return rest
	}
	return rest[:end]
}
