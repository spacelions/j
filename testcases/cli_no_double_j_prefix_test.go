package testcases_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestCLI_NoDoubleJPrefix is the regression for the
// `J: J: <message>` double-prefix bug that surfaced when an
// error constructed with `errors.New("J: ...")` /
// `fmt.Errorf("J: ...")` reached the cli.Execute print boundary
// at root.go:54 (which already prepends `"J: %v\n"`). It drives a
// real failure path through cli.Execute — `j tasks resume-work`
// against a row without a saved plan, which the artifact gate
// rejects before launching an agent — and asserts that the
// stderr output contains exactly one `J: ` prefix.
func TestCLI_NoDoubleJPrefix(t *testing.T) {
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusVerifying
	})
	taskDir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.Remove(filepath.Join(taskDir, tasks.PlanFileName)); err != nil {
		t.Fatalf("remove plan: %v", err)
	}

	stderr := captureExecuteStderr(t, []string{
		"j", "tasks", "resume-work",
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
