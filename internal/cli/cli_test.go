package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/testutil"
)

// TestMain chdir's the entire cli-package test binary into an
// ephemeral directory so plan/work invocations don't write a
// .j/settings file into the source tree as a side effect.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "cli-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	if err := os.Chdir(tmp); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// resetGlobals resets any global state that tests share (the Viper
// singleton used by per-subcommand BindPFlag / BindEnv calls).
func resetGlobals(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })
}

// mustInit lays down the .j layout in the current working directory.
// CLI tests that exercise plan/work/tasks/settings via Execute must
// call this helper so the new pre-flight contract is satisfied.
// Idempotent.
func mustInit(t *testing.T) {
	t.Helper()
	testutil.Init(t)
	t.Cleanup(func() {
		jDir, err := store.DefaultDir()
		if err != nil {
			return
		}
		_ = os.RemoveAll(jDir)
	})
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	_ = w.Close()
	os.Stderr = orig
	return <-done
}

// withArgs swaps os.Args for the duration of the test.
func withArgs(t *testing.T, args ...string) {
	t.Helper()
	orig := os.Args
	os.Args = append([]string{"j"}, args...)
	t.Cleanup(func() { os.Args = orig })
}

// assertExecuteFails runs Execute and asserts it exits with code 1 and
// stderr contains every substring in wants.
func assertExecuteFails(t *testing.T, wants ...string) {
	t.Helper()
	var code int
	out := captureStderr(t, func() { code = Execute() })
	if code != 1 {
		t.Fatalf("Execute = %d, want 1; stderr = %q", code, out)
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Fatalf("stderr = %q, missing %q", out, w)
		}
	}
}

func TestExecute_Help(t *testing.T) {
	resetGlobals(t)
	withArgs(t, "--help")
	if got := Execute(); got != 0 {
		t.Fatalf("Execute = %d", got)
	}
}

func TestSettingsCommand_Children(t *testing.T) {
	cmd := settings.New()
	var setSub, resetSub bool
	for _, sub := range cmd.Commands() {
		if strings.HasPrefix(sub.Use, "set ") {
			setSub = true
		}
		if strings.HasPrefix(sub.Use, "reset ") {
			resetSub = true
		}
	}
	if !setSub || !resetSub {
		t.Fatalf("settings should expose set and reset; got set=%v reset=%v", setSub, resetSub)
	}
}


func TestExecute_RunMissingSettings(t *testing.T) {
	resetGlobals(t)
	t.Chdir(t.TempDir())
	withArgs(t, "run")
	assertExecuteFails(t, "j:", "j init")
}

func TestExecute_WebMissingSettings(t *testing.T) {
	resetGlobals(t)
	t.Chdir(t.TempDir())
	withArgs(t, "web")
	assertExecuteFails(t, "j init")
}

func TestExecute_PlanInvalidFromFile_FromFlag(t *testing.T) {
	resetGlobals(t)
	mustInit(t)
	withArgs(t, "plan", "--from-file", "/this/path/does/not/exist.md")
	assertExecuteFails(t, "j:", "stat")
}

func TestExecute_PlanInvalidFromFile_FromEnv(t *testing.T) {
	resetGlobals(t)
	mustInit(t)
	t.Setenv("PLAN_FROM_FILE", "/this/path/does/not/exist.md")
	withArgs(t, "plan")
	assertExecuteFails(t, "j:", "stat")
}

// TestExecute_PlanInteractiveFlag_FromFlag confirms --interactive is
// parsed by cobra and surfaces on the viper singleton via BindPFlag.
// We piggy-back on the invalid-from-file failure path so the test
// stays hermetic (no agent invocation), and read viper after Execute
// to observe the bound value.
func TestExecute_PlanInteractiveFlag_FromFlag(t *testing.T) {
	resetGlobals(t)
	mustInit(t)
	withArgs(t, "plan", "--interactive=false", "--from-file", "/this/path/does/not/exist.md")
	assertExecuteFails(t, "stat")
	if viper.GetBool("plan.interactive") {
		t.Fatalf("plan.interactive should be false after --interactive=false")
	}
}

func TestExecute_PlanInteractiveFlag_FromEnv(t *testing.T) {
	resetGlobals(t)
	mustInit(t)
	t.Setenv("PLAN_INTERACTIVE", "false")
	t.Setenv("PLAN_FROM_FILE", "/this/path/does/not/exist.md")
	withArgs(t, "plan")
	assertExecuteFails(t, "stat")
	if viper.GetBool("plan.interactive") {
		t.Fatalf("plan.interactive should be false from PLAN_INTERACTIVE=false")
	}
}

func TestExecute_WorkInvalidFromFile_FromFlag(t *testing.T) {
	resetGlobals(t)
	mustInit(t)
	withArgs(t, "work", "--from-file", "/this/path/does/not/exist.md")
	assertExecuteFails(t, "j:", "stat")
}

func TestExecute_WorkInvalidFromFile_FromEnv(t *testing.T) {
	resetGlobals(t)
	mustInit(t)
	t.Setenv("WORK_FROM_FILE", "/this/path/does/not/exist.md")
	withArgs(t, "work")
	assertExecuteFails(t, "j:", "stat")
}

// TestExecute_WorkInteractiveFlag_FromFlag confirms --interactive is
// parsed by cobra and surfaces on the viper singleton via BindPFlag.
// We piggy-back on the invalid-from-file failure path so the test
// stays hermetic (no agent invocation), and read viper after Execute
// to observe the bound value.
func TestExecute_WorkInteractiveFlag_FromFlag(t *testing.T) {
	resetGlobals(t)
	mustInit(t)
	withArgs(t, "work", "--interactive=false", "--from-file", "/this/path/does/not/exist.md")
	assertExecuteFails(t, "stat")
	if viper.GetBool("work.interactive") {
		t.Fatalf("work.interactive should be false after --interactive=false")
	}
}

func TestExecute_WorkInteractiveFlag_FromEnv(t *testing.T) {
	resetGlobals(t)
	mustInit(t)
	t.Setenv("WORK_INTERACTIVE", "false")
	t.Setenv("WORK_FROM_FILE", "/this/path/does/not/exist.md")
	withArgs(t, "work")
	assertExecuteFails(t, "stat")
	if viper.GetBool("work.interactive") {
		t.Fatalf("work.interactive should be false from WORK_INTERACTIVE=false")
	}
}
