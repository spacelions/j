package testcases_test

// AC: Codex worker sessions are not changed by the plan-mode request.
// Interactive Work must not receive --sandbox, --ask-for-approval, etc.

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/codex"
	"github.com/spacelions/j/internal/testutil"
)

func TestCodexWorkerInteractiveNoPlanModeFlags(t *testing.T) {
	stub := testutil.InstallExecutableStub(t, testutil.ExecutableStubOptions{
		Binary:   "codex",
		ExitCode: 0,
	})

	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte("1. step"), 0o600); err != nil {
		t.Fatal(err)
	}

	req := codingagents.WorkRequest{
		TaskDir:     dir,
		PlanPath:    planPath,
		Model:       "gpt-5.5",
		Interactive: true,
	}
	if _, err := codex.New().Work(t.Context(), req); err != nil {
		t.Fatalf("Work: %v", err)
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
				"interactive Work argv must not contain planner-only arg %q: %v",
				banned, argv,
			)
		}
	}

	for _, banned := range []string{"exec"} {
		if slices.Contains(argv, banned) {
			t.Errorf(
				"interactive Work argv must not contain %q: %v", banned, argv,
			)
		}
	}

	if !slices.Contains(argv, "--") {
		t.Errorf("argv missing prompt separator '--': %v", argv)
	}
}
