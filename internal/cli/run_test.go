package cli

import (
	"context"
	"strings"
	"testing"
)

func TestRunCmd_MissingKey(t *testing.T) {
	resetCLI(t)
	// Ensure config starts with no key by initializing explicitly.
	configFile = ""
	googleAPIKeyFL = ""
	t.Setenv("GOOGLE_API_KEY", "")
	if err := rootCmd.PersistentPreRunE(rootCmd, nil); err != nil {
		t.Fatalf("prerun: %v", err)
	}

	err := runCmd.RunE(runCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "GOOGLE_API_KEY") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunCmd_CallsWorkflow(t *testing.T) {
	resetCLI(t)
	configFile = ""
	googleAPIKeyFL = ""
	t.Setenv("GOOGLE_API_KEY", "some-key")
	t.Setenv("MODEL", "test-model")
	t.Setenv("MAX_ITERATIONS", "4")
	if err := rootCmd.PersistentPreRunE(rootCmd, nil); err != nil {
		t.Fatalf("prerun: %v", err)
	}

	var gotKey, gotModel string
	var gotIter uint
	var gotArgs []string
	workflowRun = func(_ context.Context, key, model string, iter uint, args []string) error {
		gotKey, gotModel, gotIter, gotArgs = key, model, iter, args
		return nil
	}

	if err := runCmd.RunE(runCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if gotKey != "some-key" || gotModel != "test-model" || gotIter != 4 {
		t.Fatalf("got %q %q %d", gotKey, gotModel, gotIter)
	}
	if gotArgs != nil {
		t.Fatalf("args = %v", gotArgs)
	}
}
