package testcases_test

// AC: Non-interactive Codex runs are not changed by the plan-mode request.
// Headless Plan must use the bypass flag and must not carry plan-mode flags.

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/codex"
	"github.com/spacelions/j/internal/testutil"
)

func TestCodexPlannerHeadlessNoPlanModeFlags(t *testing.T) {
	stub := testutil.InstallExecutableStub(t, testutil.ExecutableStubOptions{
		Binary:   "codex",
		Stdout:   "ok\n",
		ExitCode: 0,
	})

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("# spec"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "agent.log")

	req := codingagents.PlanRequest{
		TaskDir:                dir,
		FromFilePath:           specPath,
		Model:                  "gpt-5.5",
		RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            false,
		AgentLogPath:           logPath,
	}
	pid, err := codex.New().Plan(t.Context(), req)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("headless Plan pid = %d, want > 0", pid)
	}

	const minArgs = 6
	argv := testutil.WaitForNullArgs(
		t, stub.CallsPath, minArgs, 5*time.Second,
	)

	if !slices.Contains(argv, "exec") {
		t.Errorf("headless Plan argv must contain 'exec': %v", argv)
	}
	if !slices.Contains(argv, "--dangerously-bypass-approvals-and-sandbox") {
		t.Errorf(
			"headless Plan argv must contain bypass flag: %v", argv,
		)
	}

	for _, banned := range []string{
		"--ask-for-approval", "on-request",
		"--sandbox", "read-only",
	} {
		if slices.Contains(argv, banned) {
			t.Errorf(
				"headless Plan argv must not contain planner-mode arg %q: %v",
				banned, argv,
			)
		}
	}
}
