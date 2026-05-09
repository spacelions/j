package testcases_test

// AC: Resuming an interactive Codex planning session preserves the same
// plan-mode behavior (read-only sandbox, approval-on-request).

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/codex"
	"github.com/spacelions/j/internal/testutil"
)

func TestCodexPlannerInteractiveResumePreservesPlanMode(t *testing.T) {
	const resumeID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	stub := testutil.InstallExecutableStub(t, testutil.ExecutableStubOptions{
		Binary:   "codex",
		ExitCode: 0,
	})

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("# spec"), 0o600); err != nil {
		t.Fatal(err)
	}

	req := codingagents.PlanRequest{
		FromFilePath:           specPath,
		Model:                  "gpt-5.5",
		RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            true,
		ResumeChatID:           resumeID,
	}
	if _, err := codex.New().Plan(t.Context(), req); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	argv := testutil.ReadNullArgs(t, stub.CallsPath)
	if len(argv) == 0 {
		t.Fatal("codex stub was never called")
	}

	if !slices.Contains(argv, "resume") {
		t.Errorf("resumed Plan argv must contain 'resume': %v", argv)
	}
	if !slices.Contains(argv, resumeID) {
		t.Errorf("resumed Plan argv must contain resume ID %q: %v", resumeID, argv)
	}

	wantPairs := [][2]string{
		{"--ask-for-approval", "on-request"},
		{"--sandbox", "read-only"},
	}
	for _, pair := range wantPairs {
		flag, val := pair[0], pair[1]
		idx := slices.Index(argv, flag)
		if idx < 0 {
			t.Errorf("resumed Plan argv missing %q: %v", flag, argv)
			continue
		}
		if idx+1 >= len(argv) || argv[idx+1] != val {
			t.Errorf(
				"argv[%d]=%q but argv[%d] = %q, want %q: %v",
				idx, flag, idx+1, argv[idx+1], val, argv,
			)
		}
	}

	for _, banned := range []string{"exec"} {
		if slices.Contains(argv, banned) {
			t.Errorf(
				"interactive Plan resume argv must not contain %q: %v",
				banned, argv,
			)
		}
	}
}
