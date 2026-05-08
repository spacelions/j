package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/settings"
)

// TestMain chdir's the entire cli-package test binary into an
// ephemeral directory so plan/work invocations don't write a
// .j/settings file into the source tree as a side effect.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "cli-test-*")
	if err != nil {
		panic(err)
	}
	if err := os.Chdir(tmp); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

// resetGlobals resets any global state that tests share (the Viper
// singleton used by per-subcommand BindPFlag / BindEnv calls).
func resetGlobals(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })
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
	assertExecuteFails(t, "J:", "j init")
}

func TestExecute_WebMissingSettings(t *testing.T) {
	resetGlobals(t)
	t.Chdir(t.TempDir())
	withArgs(t, "web")
	assertExecuteFails(t, "j init")
}
