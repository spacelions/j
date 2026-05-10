package testcases_test

import (
	"strings"
	"testing"
)

// TestWorkCommand_Removed pins that the top-level `j work` command
// is no longer wired into the cobra root: `j work --help` and
// `j work resume --help` both fail with cobra's
// `unknown command "work" for "j"`.
//
// (`j work` was replaced by task-scoped phase commands.)
//
// Replaces testcases/work-command-removed.md.
func TestWorkCommand_Removed(t *testing.T) {
	t.Chdir(t.TempDir())

	cases := []struct {
		name string
		args []string
	}{
		{name: "work-help", args: []string{"work", "--help"}},
		{name: "work-resume-help", args: []string{"work", "resume", "--help"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runRoot(t, tc.args)
			if err == nil {
				t.Fatalf("expected error for %v; stdout=%q", tc.args, out)
			}
			if !strings.Contains(err.Error(), `unknown command "work" for "j"`) {
				t.Fatalf("err = %v, want `unknown command \"work\" for \"j\"`", err)
			}
		})
	}
}
