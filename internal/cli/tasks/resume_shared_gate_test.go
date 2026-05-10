package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storetasks "github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

func TestResumeArtifactGates(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, nil)
	row := readTaskFromBolt(t, id)
	if err := requireRequirementsOrLinear(row); err != nil {
		t.Fatalf("requirements gate: %v", err)
	}
	if err := requirePlan(row); err != nil {
		t.Fatalf("plan gate: %v", err)
	}
	row.WorkBeginAt = time.Now().UTC()
	if err := requirePlanAndPriorWork(row); err != nil {
		t.Fatalf("verify gate with WorkBeginAt: %v", err)
	}
	row.WorkBeginAt = time.Time{}
	row.WorkResumeSession = "work-session"
	if err := requirePlanAndPriorWork(row); err != nil {
		t.Fatalf("verify gate with WorkResumeSession: %v", err)
	}
	row.WorkResumeSession = ""
	row.Status = storetasks.StatusWorkDone
	row.WorkTool = "codex"
	if err := requirePlanAndPriorWork(row); err != nil {
		t.Fatalf("verify gate with worker metadata: %v", err)
	}
	row.WorkTool = ""
	if err := requirePlanAndPriorWork(row); err == nil ||
		!strings.Contains(err.Error(), "no prior worker run") {
		t.Fatalf("verify gate without worker metadata = %v", err)
	}
}

func TestResumeArtifactGateFailures(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, nil)
	taskDir, err := storetasks.EnsureDir(id)
	if err != nil {
		t.Fatal(err)
	}
	row := readTaskFromBolt(t, id)
	if err := os.Remove(filepath.Join(taskDir, storetasks.RequirementsFileName)); err != nil {
		t.Fatal(err)
	}
	if err := requireRequirementsOrLinear(row); err == nil ||
		!strings.Contains(err.Error(), "requirements.md missing") {
		t.Fatalf("requirements err = %v", err)
	}
	row.LinearIssue = "SPA-86"
	if err := requireRequirementsOrLinear(row); err != nil {
		t.Fatalf("linear requirement bypass: %v", err)
	}
	if err := os.Remove(filepath.Join(taskDir, storetasks.PlanFileName)); err != nil {
		t.Fatal(err)
	}
	if err := requirePlan(row); err == nil ||
		!strings.Contains(err.Error(), "plan.md missing") {
		t.Fatalf("plan err = %v", err)
	}
	if err := requirePlanAndPriorWork(row); err == nil ||
		!strings.Contains(err.Error(), "plan.md missing") {
		t.Fatalf("verify plan err = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(taskDir, storetasks.PlanFileName),
		[]byte("plan"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := requirePlanAndPriorWork(row); err == nil ||
		!strings.Contains(err.Error(), "no prior worker run") {
		t.Fatalf("verify worker err = %v", err)
	}
}
