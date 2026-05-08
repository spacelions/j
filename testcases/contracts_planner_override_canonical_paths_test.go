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

// TestContracts_PlannerOverride_CanonicalPaths pins AC#1.1: even when
// planner.md is replaced with a body that mentions none of the
// canonical filenames, the orchestrator-composed prompt still names
// requirements.md, plan.md, and clarification.md at their canonical
// task-dir locations and carries the "Save … Then exit." save
// instruction. This is the regression guard for "user customisation
// cannot break the file-IO contract".
func TestContracts_PlannerOverride_CanonicalPaths(t *testing.T) {
	freshInit(t)

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	override := filepath.Join(dir, "planner_stub.md")
	if err := os.WriteFile(
		override, []byte("You are a planner.\n"), 0o644,
	); err != nil {
		t.Fatalf("write override: %v", err)
	}
	if _, _, err := testutil.RunCobra(t,
		settings.New(), "set", "planner.prompt="+override,
	); err != nil {
		t.Fatalf("set planner.prompt: %v", err)
	}

	const (
		req     = "/abs/.j/tasks/T1/requirements.md"
		plan    = "/abs/.j/tasks/T1/plan.md"
		clarify = "/abs/.j/tasks/T1/clarification.md"
	)
	for name, prompt := range map[string]string{
		"fresh": prompts.AppendPlannerSaveSuffix(
			prompts.BuildPlanner(req, nil), req, plan, clarify,
		),
		"resume": prompts.AppendPlannerSaveSuffix(
			prompts.BuildPlannerResume(req, nil), req, plan, clarify,
		),
	} {
		for _, want := range []string{
			req, plan, clarify,
			"Save the (possibly refined) requirements summary to",
			"Save the plan to",
			"Then exit.",
			"If you need clarification",
		} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s prompt missing %q despite stub planner override: %q",
					name, want, prompt)
			}
		}
	}
}
