package planner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestReadPlanArtifacts_PlanPathNonMissingError mirrors
// TestVerify_AC5_NonMissingReadErrorUsesDangerousText for the
// plan.md branch: a non-missing read failure (here, a directory in
// place of the file) renders as plain dangerous text rather than a
// boxed warning, and the function still returns the (possibly
// empty) refined requirements.
func TestReadPlanArtifacts_PlanPathNonMissingError(t *testing.T) {
	dir := t.TempDir()
	reqPath := filepath.Join(dir, "requirements.md")
	if err := os.WriteFile(reqPath, []byte("req-ok"), 0o644); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	planPath := filepath.Join(dir, "plan.md")
	if err := os.Mkdir(planPath, 0o755); err != nil {
		t.Fatalf("mkdir plan path: %v", err)
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
	if strings.Contains(stripped, "missing planner artifact") {
		t.Fatalf("stderr = %q, contains missing-message for non-missing read error", stripped)
	}
	if !strings.Contains(stripped, "J: read "+planPath) {
		t.Fatalf("stderr = %q, want 'J: read %s' prefix", stripped, planPath)
	}
}
