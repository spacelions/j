package planner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestVerify_AC5_NonMissingReadErrorUsesDangerousText verifies
// acceptance criterion 5:
// Read failures other than "does not exist" remain dangerous warnings,
// because they may indicate permission or filesystem problems, but they
// are printed as dangerous text rather than boxed warning dialogs.
func TestVerify_AC5_NonMissingReadErrorUsesDangerousText(t *testing.T) {
	dir := t.TempDir()

	// Create a directory where a file is expected — ReadFile will
	// return an *os.PathError with the underlying syscall.EISDIR
	// (not fs.ErrNotExist).
	reqPath := filepath.Join(dir, "requirements.md")
	if err := os.Mkdir(reqPath, 0o755); err != nil {
		t.Fatalf("mkdir requirements path: %v", err)
	}
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte("plan-ok"), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	var stderr bytes.Buffer
	gotReq, gotPlan := readPlanArtifacts(
		&stderr, nil, reqPath, planPath,
	)

	// Read of the directory-as-file should fail: refinedReq empty.
	if gotReq != "" {
		t.Fatalf("refinedReq = %q, want empty", gotReq)
	}
	// Plan succeeded: planMD populated.
	if gotPlan != "plan-ok" {
		t.Fatalf("planMD = %q, want plan-ok", gotPlan)
	}

	stripped := ansi.Strip(stderr.String())

	// Must NOT contain dialog box borders.
	for _, glyph := range []string{"┌", "┐", "└", "┘"} {
		if strings.Contains(stripped, glyph) {
			t.Fatalf("stderr contains dialog border glyph %q: %q", glyph, stripped)
		}
	}
	// Must NOT contain the normal missing-artifact message for the
	// read error (it's not a missing file).
	if strings.Contains(stripped, "missing planner artifact") {
		t.Fatalf("stderr = %q, contains 'missing planner artifact' for a non-missing read error", stripped)
	}
	// Must contain a read-error prefix indicating dangerous output.
	if !strings.Contains(stripped, "J: read "+reqPath) {
		t.Fatalf("stderr = %q, want dangerous read-error prefix 'J: read %s'", stripped, reqPath)
	}
}
