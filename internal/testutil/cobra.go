package testutil

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

// RunCobra drives cmd in-process with the test's context and returns
// the stdout/stderr payloads alongside the error from Execute. It
// mirrors the inlined runner in internal/cli/tasks/cmd_test.go so
// e2e tests can reuse the same invocation contract without repeating
// SetOut/SetErr/SetContext per call site.
func RunCobra(
	t *testing.T, cmd *cobra.Command, args ...string,
) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(t.Context())
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}
