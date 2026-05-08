package testcases_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/agents/instructions"
	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/testutil"
)

// TestSettingsSet_PromptCreatesParentDirs pins AC#1's "parents created"
// guarantee: nested missing directories are minted before the seed
// file is written.
func TestSettingsSet_PromptCreatesParentDirs(t *testing.T) {
	freshInit(t)

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	nested := filepath.Join(dir, "deep", "nested", "tree", "worker.md")
	if _, err := os.Stat(filepath.Dir(nested)); !os.IsNotExist(err) {
		t.Fatalf("precondition: parent should not exist yet")
	}

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "worker.prompt="+nested,
	); err != nil {
		t.Fatalf("set: %v", err)
	}

	body, err := os.ReadFile(nested)
	if err != nil {
		t.Fatalf("read seeded nested file: %v", err)
	}
	if string(body) != instructions.Worker {
		t.Fatalf("nested seed body mismatch")
	}
}
