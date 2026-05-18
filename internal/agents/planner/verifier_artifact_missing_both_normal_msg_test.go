package planner

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestVerify_AC3_MissingBothPrintsNormalMessages verifies
// acceptance criterion 3:
// If both planner artifacts are missing, both missing artifacts are
// reported without using the dangerous warning style.
func TestVerify_AC3_MissingBothPrintsNormalMessages(t *testing.T) {
	dir := t.TempDir()
	reqPath := filepath.Join(dir, "requirements.md")
	planPath := filepath.Join(dir, "plan.md")

	var stderr bytes.Buffer
	gotReq, gotPlan := readPlanArtifacts(
		&stderr, nil, reqPath, planPath,
	)

	if gotReq != "" {
		t.Fatalf("refinedReq = %q, want empty", gotReq)
	}
	if gotPlan != "" {
		t.Fatalf("planMD = %q, want empty", gotPlan)
	}

	stripped := ansi.Strip(stderr.String())

	// Both missing-artifact messages must appear.
	if !strings.Contains(stripped, "J: missing planner artifact "+reqPath) {
		t.Fatalf("stderr = %q, want message for %q", stripped, reqPath)
	}
	if !strings.Contains(stripped, "J: missing planner artifact "+planPath) {
		t.Fatalf("stderr = %q, want message for %q", stripped, planPath)
	}
	// Exactly two missing-artifact messages, no dangerous borders.
	if strings.Count(stripped, "missing planner artifact") != 2 {
		t.Fatalf("expected exactly 2 missing-artifact messages, got %q", stripped)
	}
	for _, glyph := range []string{"┌", "┐", "└", "┘"} {
		if strings.Contains(stripped, glyph) {
			t.Fatalf("stderr contains dialog border glyph %q: %q", glyph, stripped)
		}
	}
}
