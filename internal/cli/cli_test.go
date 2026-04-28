package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// resetGlobals resets any global state that tests share (currently only the
// Viper singleton used by internal/config).
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

func TestExecute_Help(t *testing.T) {
	resetGlobals(t)
	withArgs(t, "--help")
	if got := Execute(); got != 0 {
		t.Fatalf("Execute = %d", got)
	}
}

func TestExecute_RunMissingKey(t *testing.T) {
	resetGlobals(t)
	t.Setenv("GOOGLE_API_KEY", "")
	withArgs(t, "run")

	var code int
	out := captureStderr(t, func() { code = Execute() })
	if code != 1 {
		t.Fatalf("Execute = %d, want 1", code)
	}
	if !strings.Contains(out, "j:") || !strings.Contains(out, "GOOGLE_API_KEY") {
		t.Fatalf("stderr = %q", out)
	}
}

func TestExecute_WebMissingKey(t *testing.T) {
	resetGlobals(t)
	t.Setenv("GOOGLE_API_KEY", "")
	withArgs(t, "web")

	var code int
	out := captureStderr(t, func() { code = Execute() })
	if code != 1 {
		t.Fatalf("Execute = %d, want 1", code)
	}
	if !strings.Contains(out, "GOOGLE_API_KEY") {
		t.Fatalf("stderr = %q", out)
	}
}

func TestExecute_PlanInvalidTarget_FromFlag(t *testing.T) {
	resetGlobals(t)
	withArgs(t, "plan", "--target", "/this/path/does/not/exist.md")

	var code int
	out := captureStderr(t, func() { code = Execute() })
	if code != 1 {
		t.Fatalf("Execute = %d, want 1", code)
	}
	if !strings.Contains(out, "j:") || !strings.Contains(out, "stat") {
		t.Fatalf("stderr = %q", out)
	}
}

func TestExecute_PlanInvalidTarget_FromEnv(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PLAN_TARGET", "/this/path/does/not/exist.md")
	withArgs(t, "plan")

	var code int
	out := captureStderr(t, func() { code = Execute() })
	if code != 1 {
		t.Fatalf("Execute = %d, want 1", code)
	}
	if !strings.Contains(out, "j:") || !strings.Contains(out, "stat") {
		t.Fatalf("stderr = %q", out)
	}
}

// TestExecute_PlanInteractiveFlag_FromFlag confirms --interactive is
// parsed by cobra and surfaces on the viper singleton via BindPFlag.
// We piggy-back on the invalid-target failure path so the test stays
// hermetic (no agent invocation), and read viper after Execute to
// observe the bound value.
func TestExecute_PlanInteractiveFlag_FromFlag(t *testing.T) {
	resetGlobals(t)
	withArgs(t, "plan", "--interactive=false", "--target", "/this/path/does/not/exist.md")

	var code int
	out := captureStderr(t, func() { code = Execute() })
	if code != 1 {
		t.Fatalf("Execute = %d, want 1", code)
	}
	if !strings.Contains(out, "stat") {
		t.Fatalf("stderr = %q", out)
	}
	if viper.GetBool("plan.interactive") {
		t.Fatalf("plan.interactive should be false after --interactive=false")
	}
}

func TestExecute_PlanInteractiveFlag_FromEnv(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PLAN_INTERACTIVE", "false")
	t.Setenv("PLAN_TARGET", "/this/path/does/not/exist.md")
	withArgs(t, "plan")

	var code int
	out := captureStderr(t, func() { code = Execute() })
	if code != 1 {
		t.Fatalf("Execute = %d, want 1", code)
	}
	if !strings.Contains(out, "stat") {
		t.Fatalf("stderr = %q", out)
	}
	if viper.GetBool("plan.interactive") {
		t.Fatalf("plan.interactive should be false from PLAN_INTERACTIVE=false")
	}
}
