package testcases_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/prompts"
	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/testutil"
)

// TestSettingsSet_PromptRuntimeRender pins AC#4: with the override set
// (and the seeded file replaced with custom content), every shell-out
// builder renders the override body. BuildVerifierFix uses the worker
// prompt so the worker override governs the fix loop.
func TestSettingsSet_PromptRuntimeRender(t *testing.T) {
	freshInit(t)

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	type role struct {
		bucket string
		path   string
		body   string
	}
	roles := []role{
		{"planner", filepath.Join(dir, "p.md"), "PLANNER OVR BODY"},
		{"worker", filepath.Join(dir, "w.md"), "WORKER OVR BODY"},
		{"verifier", filepath.Join(dir, "v.md"), "VERIFIER OVR BODY"},
	}
	for _, r := range roles {
		if _, _, err := testutil.RunCobra(settings.New(),
			"set", r.bucket+".prompt="+r.path,
		); err != nil {
			t.Fatalf("set %s: %v", r.bucket, err)
		}
		if err := os.WriteFile(r.path, []byte(r.body), 0o644); err != nil {
			t.Fatalf("write %s body: %v", r.bucket, err)
		}
	}

	plannerOut := prompts.BuildPlanner("/tmp/feature.md", nil)
	if !strings.Contains(plannerOut, roles[0].body) {
		t.Fatalf("BuildPlanner missing override: %q", plannerOut)
	}
	plannerResume := prompts.BuildPlannerResume("/tmp/feature.md", nil)
	if !strings.Contains(plannerResume, roles[0].body) {
		t.Fatalf("BuildPlannerResume missing override: %q", plannerResume)
	}

	workerOut := prompts.BuildWorker("/tmp/plan.md", "", nil)
	if !strings.Contains(workerOut, roles[1].body) {
		t.Fatalf("BuildWorker missing override: %q", workerOut)
	}
	workerResume := prompts.BuildWorkerResume("/tmp/plan.md", "", nil)
	if !strings.Contains(workerResume, roles[1].body) {
		t.Fatalf("BuildWorkerResume missing override: %q", workerResume)
	}
	fixOut := prompts.BuildVerifierFix("/tmp/plan.md", "/tmp/v.md", "")
	if !strings.Contains(fixOut, roles[1].body) {
		t.Fatalf("BuildVerifierFix should honour worker override: %q",
			fixOut)
	}

	verifierOut := prompts.BuildVerifier(
		"r.md", "p.md", "vp.md", "vf.md", "", nil,
	)
	if !strings.Contains(verifierOut, roles[2].body) {
		t.Fatalf("BuildVerifier missing override: %q", verifierOut)
	}
	verifierResume := prompts.BuildVerifierResume(
		"r.md", "p.md", "", nil,
	)
	if !strings.Contains(verifierResume, roles[2].body) {
		t.Fatalf("BuildVerifierResume missing override: %q",
			verifierResume)
	}
}
