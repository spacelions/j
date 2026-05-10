package testcases_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		if _, _, err := testutil.RunCobra(t, settings.New(),
			"set", r.bucket+".prompt="+r.path,
		); err != nil {
			t.Fatalf("set %s: %v", r.bucket, err)
		}
		if err := os.WriteFile(r.path, []byte(r.body), 0o644); err != nil {
			t.Fatalf("write %s body: %v", r.bucket, err)
		}
	}

	plannerOut := buildPlannerPrompt("/tmp/feature.md", nil)
	if !strings.Contains(plannerOut, roles[0].body) {
		t.Fatalf("BuildPlanner missing override: %q", plannerOut)
	}
	const clarify = "/tmp/c.md"
	plannerResume := buildPlannerResumePrompt(
		"/tmp/feature.md", nil,
	)
	if !strings.Contains(plannerResume, roles[0].body) {
		t.Fatalf("BuildPlannerResume missing override: %q", plannerResume)
	}

	workerOut := buildWorkerPrompt(
		"/tmp/plan.md", "", nil, clarify,
	)
	if !strings.Contains(workerOut, roles[1].body) {
		t.Fatalf("BuildWorker missing override: %q", workerOut)
	}
	workerResume := buildWorkerResumePrompt(
		"/tmp/plan.md", "", nil, clarify,
	)
	if !strings.Contains(workerResume, roles[1].body) {
		t.Fatalf("BuildWorkerResume missing override: %q", workerResume)
	}
	fixOut := buildVerifierFixPrompt(
		"/tmp/plan.md", "/tmp/v.md", "", clarify,
	)
	if !strings.Contains(fixOut, roles[1].body) {
		t.Fatalf("BuildVerifierFix should honour worker override: %q",
			fixOut)
	}

	verifierOut := buildVerifierPrompt(
		"r.md", "p.md", "vp.md", "vf.md", "", nil, clarify,
	)
	if !strings.Contains(verifierOut, roles[2].body) {
		t.Fatalf("BuildVerifier missing override: %q", verifierOut)
	}
	verifierResume := buildVerifierResumePrompt(
		"r.md", "p.md", "", nil, clarify,
	)
	if !strings.Contains(verifierResume, roles[2].body) {
		t.Fatalf("BuildVerifierResume missing override: %q",
			verifierResume)
	}
}
