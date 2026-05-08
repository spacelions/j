package testcases_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/testutil"
)

// TestSettingsSet_PromptExistingFileNotOverwritten pins AC#1's
// existing-file branch: when <path> already exists, contents are
// unchanged and no copy line is emitted.
func TestSettingsSet_PromptExistingFileNotOverwritten(t *testing.T) {
	freshInit(t)

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dest := filepath.Join(dir, "user-edited-planner.md")
	const original = "user-edited body that must not be overwritten\n"
	if err := os.WriteFile(dest, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stdout, _, err := testutil.RunCobra(t, settings.New(),
		"set", "planner.prompt="+dest,
	)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if strings.Contains(stdout, "wrote default prompt") {
		t.Fatalf("stdout = %q, must not seed when file exists", stdout)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != original {
		t.Fatalf("file body changed: got %q, want %q", got, original)
	}
}
