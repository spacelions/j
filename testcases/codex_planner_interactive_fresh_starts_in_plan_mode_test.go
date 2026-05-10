package testcases_test

// AC: When Codex is used for an interactive planning session, the session
// starts with read-only sandboxing enabled and Codex is configured to ask
// for approval when the model determines approval is needed.

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

func TestCodexPlannerInteractiveFreshStartsInPlanMode(t *testing.T) {
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
		TaskDir:                dir,
		FromFilePath:           specPath,
		Model:                  "gpt-5.5",
		RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            true,
	}
	if _, err := codex.New().Plan(t.Context(), req); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var argv []string
	for {
		argv = testutil.ReadNullArgsBestEffort(stub.CallsPath)
		if len(argv) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(argv) == 0 {
		t.Fatal("codex stub was never called")
	}

	wantPairs := [][2]string{
		{"--ask-for-approval", "on-request"},
		{"--sandbox", "read-only"},
	}
	for _, pair := range wantPairs {
		flag, val := pair[0], pair[1]
		idx := slices.Index(argv, flag)
		if idx < 0 {
			t.Errorf("argv missing %q: %v", flag, argv)
			continue
		}
		if idx+1 >= len(argv) || argv[idx+1] != val {
			t.Errorf(
				"argv[%d]=%q but argv[%d] = %q, want %q: %v",
				idx, flag, idx+1, argv[idx+1], val, argv,
			)
		}
	}

	for _, banned := range []string{"exec", "resume"} {
		if slices.Contains(argv, banned) {
			t.Errorf(
				"fresh interactive Plan argv must not contain %q: %v",
				banned, argv,
			)
		}
	}

	if !slices.Contains(argv, "--") {
		t.Errorf("argv missing prompt separator '--': %v", argv)
	}
}
