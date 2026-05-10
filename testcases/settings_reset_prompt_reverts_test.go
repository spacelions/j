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

// TestSettingsReset_PromptRevertsToEmbedded pins AC#5: after
// `j settings reset <role>.prompt`, BuildPlanner renders the embedded
// default. The on-disk markdown copy survives the reset.
func TestSettingsReset_PromptRevertsToEmbedded(t *testing.T) {
	freshInit(t)

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dest := filepath.Join(dir, "planner.md")
	const customBody = "PLANNER OVERRIDE BODY"

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"set", "planner.prompt="+dest,
	); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := os.WriteFile(dest, []byte(customBody), 0o644); err != nil {
		t.Fatalf("write override body: %v", err)
	}

	beforeReset := buildPlannerPrompt("/tmp/feature.md", nil)
	if !strings.Contains(beforeReset, customBody) {
		t.Fatalf("setup: BuildPlanner did not honour override: %q",
			beforeReset)
	}

	if _, _, err := testutil.RunCobra(t, settings.New(),
		"reset", "planner.prompt",
	); err != nil {
		t.Fatalf("reset: %v", err)
	}

	afterReset := buildPlannerPrompt("/tmp/feature.md", nil)
	if strings.Contains(afterReset, customBody) {
		t.Fatalf("reset did not revert: %q", afterReset)
	}
	if !strings.Contains(
		afterReset, strings.TrimSpace(instructions.Planner),
	) {
		t.Fatalf("post-reset BuildPlanner missing embedded body: %q",
			afterReset)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("on-disk copy must survive reset: stat err = %v", err)
	}
}
