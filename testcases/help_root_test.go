package testcases_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli"
	"github.com/spacelions/j/internal/testutil"
)

// rootSubcommands is the set of top-level subcommands wired into the
// cobra root by cli.NewRoot, plus the two cobra adds automatically
// (`completion`, `help`). The original prose checklist also listed
// `plan`, `verify`, and `work`; those have been removed by recent
// refactors so the assertion was updated to match the wired root.
// (Doc drift in testcases/help-root.md was the trigger.)
var rootSubcommands = []string{
	"completion",
	"help",
	"init",
	"run",
	"settings",
	"tasks",
	"web",
}

const rootHelpFooter = `Use "j [command] --help" for more ` +
	`information about a command.`

// TestHelpRoot pins the two help invocations the prose checklist
// covered: `j help` and `j --help`. Both must exit 0, start with the
// short blurb, list every wired top-level subcommand, and terminate
// with the canonical cobra footer.
//
// Replaces testcases/help-root.md.
func TestHelpRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	cases := []struct {
		name string
		args []string
	}{
		{name: "help", args: []string{"help"}},
		{name: "flag", args: []string{"--help"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runRoot(t, tc.args)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			assertRootHelp(t, out)
		})
	}
}

// runRoot drives cli.NewRoot in-process with stdout / stderr captured
// to a single buffer (cobra writes the help text to stdout via
// SetOut). The returned string is the full stdout payload.
func runRoot(t *testing.T, args []string) (string, error) {
	t.Helper()
	cmd := cli.NewRoot()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(t.Context())
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// assertRootHelp pins the three invariants the prose checklist
// asserted: prefix, subcommand list, footer. Trailing newline is
// stripped before the suffix check so the assertion does not depend
// on whether cobra appends one.
func assertRootHelp(t *testing.T, out string) {
	t.Helper()
	if !strings.HasPrefix(out, "J Harness CLI") {
		t.Fatalf("stdout missing prefix %q: %q", "J Harness CLI", out)
	}
	for _, sub := range rootSubcommands {
		if !strings.Contains(out, sub) {
			t.Fatalf("stdout missing subcommand %q: %q", sub, out)
		}
	}
	trimmed := strings.TrimRight(out, "\n")
	if !strings.HasSuffix(trimmed, rootHelpFooter) {
		t.Fatalf("stdout missing footer %q: %q", rootHelpFooter, out)
	}
}
