package testcases_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
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

// installCursorAgentLoginStub drops a PATH-resolvable `cursor-agent`
// shell script that prints "Logged in" and exits 0 so the start-time
// PreRunE login check (`cursor-agent status`) succeeds without the
// real binary on CI. Mirrors the stub in
// internal/cli/tasks/continue_test.go. Skips on Windows because the
// shebang stub is POSIX-only.
func installCursorAgentLoginStub(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("posix-only stub")
	}
	dir := t.TempDir()
	body := "#!/bin/sh\nprintf 'Logged in\\n'\nexit 0\n"
	bin := filepath.Join(dir, "cursor-agent")
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH",
		dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
