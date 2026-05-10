package testcases_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli"
)

// TestCLI_NoDoubleJPrefix is the regression for the
// `J: J: <message>` double-prefix bug that surfaced when an
// error constructed with `errors.New("J: ...")` /
// `fmt.Errorf("J: ...")` reached the cli.Execute print boundary
// at root.go:54 (which already prepends `"J: %v\n"`). It drives a
// real synchronous failure path through cli.Execute — `j tasks
// orchestrate` without the required --id flag fails immediately
// with "tasks: orchestrate requires --id"; root.go then wraps
// this in "J: tasks: orchestrate requires --id". The test asserts
// that the stderr output contains exactly one `J: ` prefix and
// does not contain `J: J:`.
//
// resume-work is no longer usable here because it opens a TUI
// task-picker before reaching the artifact gate; a CLI-level test
// of that path would hang on stdin. The orchestrate --id-required
// error exercises the same root.go boundary without blocking.
func TestCLI_NoDoubleJPrefix(t *testing.T) {
	recoverySetupEnv(t)

	stderr := captureExecuteStderr(t, []string{
		"j", "tasks", "orchestrate",
	})

	if !strings.Contains(stderr, "J: ") {
		t.Fatalf("stderr missing single J: prefix: %q", stderr)
	}
	if strings.Contains(stderr, "J: J:") { //nolint:dupword // intentionally checking for a double-prefix bug
		t.Fatalf("stderr has double J: prefix: %q", stderr)
	}
}

// captureExecuteStderr swaps os.Stderr/os.Args for the duration
// of cli.Execute so the J: %v print boundary at root.go:54
// writes into a pipe we can read back. The test asserts on the
// captured payload without depending on the process exit code,
// which can vary across CI shells.
func captureExecuteStderr(t *testing.T, argv []string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStderr, origArgs := os.Stderr, os.Args
	os.Stderr = w
	os.Args = argv
	t.Cleanup(func() {
		os.Stderr = origStderr
		os.Args = origArgs
	})

	_ = cli.Execute()
	_ = w.Close()

	buf, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(buf)
}
