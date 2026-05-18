package planner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestVerify_AC2_MissingPlanPrintsNormalMessage verifies
// acceptance criterion 2:
// When the planner exits successfully but the plan artifact is missing,
// the CLI prints a normal, non-dangerous message that names the missing
// plan artifact.
func TestVerify_AC2_MissingPlanPrintsNormalMessage(t *testing.T) {
	dir := t.TempDir()
	reqPath := filepath.Join(dir, "requirements.md")
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(reqPath, []byte("req-ok"), 0o644); err != nil {
		t.Fatalf("write requirements: %v", err)
	}

	var stderr bytes.Buffer
	gotReq, gotPlan := readPlanArtifacts(
		&stderr, nil, reqPath, planPath,
	)

	if gotReq != "req-ok" {
		t.Fatalf("refinedReq = %q, want req-ok", gotReq)
	}
	if gotPlan != "" {
		t.Fatalf("planMD = %q, want empty", gotPlan)
	}

	stripped := ansi.Strip(stderr.String())
	want := "J: missing planner artifact " + planPath
	if !strings.Contains(stripped, want) {
		t.Fatalf("stderr = %q, want missing-artifact message %q", stripped, want)
	}
	for _, glyph := range []string{"┌", "┐", "└", "┘"} {
		if strings.Contains(stripped, glyph) {
			t.Fatalf("stderr contains dialog border glyph %q: %q", glyph, stripped)
		}
	}
	if strings.Count(stripped, "missing planner artifact") != 1 {
		t.Fatalf("expected exactly 1 missing-artifact message, got %q", stripped)
	}
}
