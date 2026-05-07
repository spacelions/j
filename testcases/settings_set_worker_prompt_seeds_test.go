package testcases_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/instructions"
	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/testutil"
)

// TestSettingsSet_WorkerPromptSeedsBundled pins AC#1 for worker:
// `j settings set worker.prompt=<path>` writes the bundled worker.md
// content to <path> when the file does not yet exist.
func TestSettingsSet_WorkerPromptSeedsBundled(t *testing.T) {
	freshInit(t)

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dest := filepath.Join(dir, "custom-worker.md")

	stdout, _, err := testutil.RunCobra(settings.New(),
		"set", "worker.prompt="+dest,
	)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if !strings.Contains(stdout, "wrote default prompt to "+dest) {
		t.Fatalf("stdout = %q, want copy-on-set echo", stdout)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read seeded file: %v", err)
	}
	if string(body) != instructions.Worker {
		t.Fatalf("seeded body did not match embedded worker.md")
	}
}
