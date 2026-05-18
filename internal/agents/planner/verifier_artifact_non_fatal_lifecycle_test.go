package planner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestVerify_AC4_MissingArtifactNonFatal verifies acceptance
// criterion 4:
// Missing artifact reporting remains non-fatal; planner lifecycle
// finalization still completes using any artifacts that are present.
//
// We confirm that readPlanArtifacts returns empty strings for missing
// files (not an error), and that the present artifact is still
// returned so the lifecycle has data to work with.
func TestVerify_AC4_MissingArtifactNonFatal(t *testing.T) {
	dir := t.TempDir()
	reqPath := filepath.Join(dir, "requirements.md")
	planPath := filepath.Join(dir, "plan.md")

	// No files exist.  readPlanArtifacts must return empties,
	// not panic or error.
	var stderr bytes.Buffer
	gotReq, gotPlan := readPlanArtifacts(
		&stderr, nil, reqPath, planPath,
	)

	if gotReq != "" {
		t.Fatalf("refinedReq = %q, want empty on missing", gotReq)
	}
	if gotPlan != "" {
		t.Fatalf("planMD = %q, want empty on missing", gotPlan)
	}

	// Messages must be present — but the function returned
	// successfully, confirming non-fatal behaviour.
	stripped := ansi.Strip(stderr.String())
	if strings.Count(stripped, "missing planner artifact") != 2 {
		t.Fatalf("expected 2 missing-artifact messages, got %q", stripped)
	}

	// Second scenario: only one artifact present.
	dir2 := t.TempDir()
	reqPath2 := filepath.Join(dir2, "requirements.md")
	planPath2 := filepath.Join(dir2, "plan.md")
	if err := os.WriteFile(planPath2, []byte("only-plan"), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	var stderr2 bytes.Buffer
	gotReq2, gotPlan2 := readPlanArtifacts(
		&stderr2, nil, reqPath2, planPath2,
	)

	// Present artifact is returned; missing is empty — not an error.
	if gotReq2 != "" {
		t.Fatalf("refinedReq = %q, want empty", gotReq2)
	}
	if gotPlan2 != "only-plan" {
		t.Fatalf("planMD = %q, want only-plan", gotPlan2)
	}
	stripped2 := ansi.Strip(stderr2.String())
	if !strings.Contains(stripped2, "missing planner artifact") {
		t.Fatalf("expected at least one missing message, got %q", stripped2)
	}
}
