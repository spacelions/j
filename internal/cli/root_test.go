package cli

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

// resetCLI captures package-level state that tests mutate and restores it.
func resetCLI(t *testing.T) {
	t.Helper()
	oWF := workflowRun
	oCF := configFile
	oKey := googleAPIKeyFL
	t.Cleanup(func() {
		workflowRun = oWF
		configFile = oCF
		googleAPIKeyFL = oKey
		// Reset root command args so subsequent tests/runs start clean.
		rootCmd.SetArgs(nil)
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

func TestExecute_Success(t *testing.T) {
	resetCLI(t)
	// Short-circuit any invocation of Run via the spy.
	workflowRun = func(context.Context, string, string, uint, []string) error { return nil }

	rootCmd.SetArgs([]string{"--help"})
	if got := Execute(); got != 0 {
		t.Fatalf("Execute = %d", got)
	}
}

func TestExecute_Error_PrintsToStderr(t *testing.T) {
	resetCLI(t)
	t.Setenv("GOOGLE_API_KEY", "")

	// Make sure no flag override is present.
	googleAPIKeyFL = ""
	rootCmd.SetArgs([]string{"run"})

	var code int
	out := captureStderr(t, func() { code = Execute() })

	if code != 1 {
		t.Fatalf("Execute = %d, want 1", code)
	}
	if !strings.Contains(out, "j:") || !strings.Contains(out, "GOOGLE_API_KEY") {
		t.Fatalf("stderr = %q", out)
	}
}

func TestPersistentPreRunE_ConfigInitError(t *testing.T) {
	resetCLI(t)

	configFile = "/nonexistent/does-not-exist.yaml"
	err := rootCmd.PersistentPreRunE(rootCmd, nil)
	if err == nil {
		t.Fatal("expected error from missing config file")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPersistentPreRunE_Success(t *testing.T) {
	resetCLI(t)
	configFile = ""
	googleAPIKeyFL = ""
	if err := rootCmd.PersistentPreRunE(rootCmd, nil); err != nil {
		t.Fatalf("PreRunE: %v", err)
	}
}
