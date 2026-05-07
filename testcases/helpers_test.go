package testcases_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/cli/initcmd"
)

// runWithStdin runs cmd with the supplied args, leaving the caller's
// existing SetIn (typically a strings.NewReader for line-based
// prompts). It mirrors testutil.RunCobra but does NOT overwrite stdin.
func runWithStdin(cmd *cobra.Command, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// freshInit chdirs the test into a fresh tempdir and runs
// `j init --yes --must-read=` against it. The four-section settings
// view that the prose checklists assume (project / planner / worker /
// verifier with the seeded must_read, plan_requires_approval, and
// max_iterations rows under [project]) is the postcondition.
func freshInit(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	mustRead := ""
	if err := initcmd.Run(context.Background(), initcmd.Options{
		Yes:      true,
		MustRead: &mustRead,
		Stdin:    nil,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	}); err != nil {
		t.Fatalf("freshInit: %v", err)
	}
}
