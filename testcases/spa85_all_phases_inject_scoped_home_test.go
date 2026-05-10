package testcases_test

// spa85_all_phases_inject_scoped_home_test.go
//
// Acceptance criterion: CODEX_HOME / DEEPSEEK_HOME must be set to the
// per-task scoped directory for every phase (Plan, Work, Verify), not
// just Plan. If Work or Verify ran with the user's real home, any two
// concurrent tasks writing sessions simultaneously would pollute each
// other's stores.
//
// Black-box: install a stub binary that records its environment, drive
// each phase, and assert the env var points at the per-task scoped home.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/codex"
	"github.com/spacelions/j/internal/coding-agents/deepseek"
	"github.com/spacelions/j/internal/testutil"
)

func TestSPA85CodexAllPhasesInjectScopedHome(t *testing.T) {
	taskDir := t.TempDir()
	stub := testutil.InstallExecutableStub(
		t,
		testutil.ExecutableStubOptions{
			Binary:    "codex",
			ExitCode:  0,
			RecordEnv: true,
		},
	)
	assertCodexScopedHome := func(t *testing.T, label string) {
		t.Helper()
		env := testutil.ReadTrimmedFile(t, stub.EnvPath)
		want := "CODEX_HOME=" + filepath.Join(taskDir, ".codex-home")
		if !strings.Contains(env, want) {
			t.Fatalf("%s: env missing %q\nenv:\n%s", label, want, env)
		}
		if err := os.Remove(stub.EnvPath); err != nil {
			t.Fatalf("cleanup env file: %v", err)
		}
	}

	specPath := filepath.Join(taskDir, "spec.md")
	if err := os.WriteFile(specPath, []byte("# spec"), 0o600); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(taskDir, "plan.md")
	if err := os.WriteFile(planPath, []byte("1. step"), 0o600); err != nil {
		t.Fatal(err)
	}
	reqPath := filepath.Join(taskDir, "requirements.md")
	if err := os.WriteFile(reqPath, []byte("req"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := codex.New()

	t.Run("Plan", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if _, err := a.Plan(t.Context(), codingagents.PlanRequest{
			TaskDir:                taskDir,
			FromFilePath:           specPath,
			Model:                  "gpt-5.5",
			RequirementsOutputPath: reqPath,
			PlanOutputPath:         planPath,
			Interactive:            true,
		}); err != nil {
			t.Fatalf("Plan: %v", err)
		}
		assertCodexScopedHome(t, "Plan")
	})

	t.Run("Work", func(t *testing.T) {
		if _, err := a.Work(t.Context(), codingagents.WorkRequest{
			TaskDir:     taskDir,
			PlanPath:    planPath,
			Model:       "gpt-5.5",
			Interactive: true,
		}); err != nil {
			t.Fatalf("Work: %v", err)
		}
		assertCodexScopedHome(t, "Work")
	})

	t.Run("Verify", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if _, err := a.Verify(t.Context(), codingagents.VerifyRequest{
			TaskDir:                    taskDir,
			RequirementsPath:           reqPath,
			PlanPath:                   planPath,
			VerifierPlanOutputPath:     filepath.Join(taskDir, "vp.md"),
			VerifierFindingsOutputPath: filepath.Join(taskDir, "vf.md"),
			Model:                      "gpt-5.5",
			Interactive:                true,
		}); err != nil {
			t.Fatalf("Verify: %v", err)
		}
		assertCodexScopedHome(t, "Verify")
	})
}

func TestSPA85DeepseekAllPhasesInjectScopedHome(t *testing.T) {
	taskDir := t.TempDir()
	stub := testutil.InstallExecutableStub(
		t,
		testutil.ExecutableStubOptions{
			Binary:    "deepseek-tui",
			ExitCode:  0,
			RecordEnv: true,
		},
	)
	assertDeepseekScopedHome := func(t *testing.T, label string) {
		t.Helper()
		env := testutil.ReadTrimmedFile(t, stub.EnvPath)
		want := "DEEPSEEK_HOME=" + filepath.Join(
			taskDir, ".deepseek-home",
		)
		if !strings.Contains(env, want) {
			t.Fatalf(
				"%s: env missing %q\nenv:\n%s", label, want, env,
			)
		}
		if err := os.Remove(stub.EnvPath); err != nil {
			t.Fatalf("cleanup env file: %v", err)
		}
	}

	specPath := filepath.Join(taskDir, "spec.md")
	if err := os.WriteFile(specPath, []byte("# spec"), 0o600); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(taskDir, "plan.md")
	if err := os.WriteFile(planPath, []byte("1. step"), 0o600); err != nil {
		t.Fatal(err)
	}
	reqPath := filepath.Join(taskDir, "requirements.md")
	if err := os.WriteFile(reqPath, []byte("req"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := deepseek.New()

	t.Run("Plan", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if _, err := a.Plan(t.Context(), codingagents.PlanRequest{
			TaskDir:                taskDir,
			FromFilePath:           specPath,
			Model:                  "deepseek-v4-pro",
			RequirementsOutputPath: reqPath,
			PlanOutputPath:         planPath,
			Interactive:            true,
		}); err != nil {
			t.Fatalf("Plan: %v", err)
		}
		assertDeepseekScopedHome(t, "Plan")
	})

	t.Run("Work", func(t *testing.T) {
		if _, err := a.Work(t.Context(), codingagents.WorkRequest{
			TaskDir:     taskDir,
			PlanPath:    planPath,
			Model:       "deepseek-v4-pro",
			Interactive: true,
		}); err != nil {
			t.Fatalf("Work: %v", err)
		}
		assertDeepseekScopedHome(t, "Work")
	})

	t.Run("Verify", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if _, err := a.Verify(t.Context(), codingagents.VerifyRequest{
			TaskDir:                    taskDir,
			RequirementsPath:           reqPath,
			PlanPath:                   planPath,
			VerifierPlanOutputPath:     filepath.Join(taskDir, "vp.md"),
			VerifierFindingsOutputPath: filepath.Join(taskDir, "vf.md"),
			Model:                      "deepseek-v4-pro",
			Interactive:                true,
		}); err != nil {
			t.Fatalf("Verify: %v", err)
		}
		assertDeepseekScopedHome(t, "Verify")
	})
}
