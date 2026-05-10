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

// TestSettingsSet_PromptMissingFileFallsBack pins AC#6: with the
// override path configured but the file absent at runtime, the
// embedded default is used (workflow does not crash) and BuildPlanner
// still renders successfully.
func TestSettingsSet_PromptMissingFileFallsBack(t *testing.T) {
	freshInit(t)

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dest := filepath.Join(dir, "vanishing-planner.md")

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "planner.prompt="+dest,
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := os.Remove(dest); err != nil {
		t.Fatalf("remove seeded file: %v", err)
	}

	got := buildPlannerPrompt("/tmp/feature.md", nil)
	if !strings.Contains(got, strings.TrimSpace(instructions.Planner)) {
		t.Fatalf("missing-file fallback: BuildPlanner did not render "+
			"embedded body: %q", got)
	}
}
