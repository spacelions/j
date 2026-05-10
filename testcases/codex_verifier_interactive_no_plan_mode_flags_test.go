package testcases_test

// AC: Codex verifier sessions are not changed by the plan-mode request.
// Interactive Verify must not receive --sandbox, --ask-for-approval, etc.

import (
	"path/filepath"
	"slices"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/codex"
	"github.com/spacelions/j/internal/testutil"
)

func TestCodexVerifierInteractiveNoPlanModeFlags(t *testing.T) {
	stub := testutil.InstallExecutableStub(t, testutil.ExecutableStubOptions{
		Binary:   "codex",
		ExitCode: 0,
	})

	dir := t.TempDir()
	t.Chdir(dir)

	req := codingagents.VerifyRequest{
		TaskDir:                    dir,
		RequirementsPath:           filepath.Join(dir, "requirements.md"),
		PlanPath:                   filepath.Join(dir, "plan.md"),
		VerifierPlanOutputPath:     filepath.Join(dir, "verifier_plan.md"),
		VerifierFindingsOutputPath: filepath.Join(dir, "verifier_findings.md"),
		Model:                      "gpt-5.5",
		Interactive:                true,
	}
	if _, err := codex.New().Verify(t.Context(), req); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	argv := testutil.ReadNullArgs(t, stub.CallsPath)
	if len(argv) == 0 {
		t.Fatal("codex stub was never called")
	}

	for _, banned := range []string{
		"--ask-for-approval", "on-request",
		"--sandbox", "read-only",
	} {
		if slices.Contains(argv, banned) {
			t.Errorf(
				"interactive Verify argv must not contain planner-only arg %q: %v",
				banned, argv,
			)
		}
	}

	for _, banned := range []string{"exec"} {
		if slices.Contains(argv, banned) {
			t.Errorf(
				"interactive Verify argv must not contain %q: %v", banned, argv,
			)
		}
	}

	if !slices.Contains(argv, "--") {
		t.Errorf("argv missing prompt separator '--': %v", argv)
	}
}
