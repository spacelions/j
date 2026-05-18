package planner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestVerify_AC1_MissingRequirementsPrintsNormalMessage verifies
// acceptance criterion 1:
// When the planner exits successfully but the requirements artifact is
// missing, the CLI prints a normal, non-dangerous message that names
// the missing requirements artifact.
func TestVerify_AC1_MissingRequirementsPrintsNormalMessage(t *testing.T) {
	dir := t.TempDir()
	reqPath := filepath.Join(dir, "requirements.md")
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte("plan-ok"), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	var stderr bytes.Buffer
	gotReq, gotPlan := readPlanArtifacts(
		&stderr, nil, reqPath, planPath,
	)

	// Artifact return: requirements should be empty, plan present.
	if gotReq != "" {
		t.Fatalf("refinedReq = %q, want empty", gotReq)
	}
	if gotPlan != "plan-ok" {
		t.Fatalf("planMD = %q, want plan-ok", gotPlan)
	}

	stripped := ansi.Strip(stderr.String())
	// Must contain the normal missing-artifact message.
	want := "J: missing planner artifact " + reqPath
	if !strings.Contains(stripped, want) {
		t.Fatalf("stderr = %q, want missing-artifact message %q", stripped, want)
	}
	// Must NOT contain dangerous dialog box borders.
	for _, glyph := range []string{"┌", "┐", "└", "┘"} {
		if strings.Contains(stripped, glyph) {
			t.Fatalf("stderr contains dialog border glyph %q: %q", glyph, stripped)
		}
	}
	// Must NOT contain a second message about plan being missing.
	if strings.Count(stripped, "missing planner artifact") != 1 {
		t.Fatalf("expected exactly 1 missing-artifact message, got %q", stripped)
	}
}
