package testcases_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/codex"
	"github.com/spacelions/j/internal/coding-agents/deepseek"
	"github.com/spacelions/j/internal/testutil"
)

func spa85FakeHome(t *testing.T, envName, subdir string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(envName, "")
	t.Setenv("HOME", root)
	home := filepath.Join(root, subdir)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	return home
}

func spa85PlanReq(
	t *testing.T, taskDir, model string,
) codingagents.PlanRequest {
	t.Helper()
	specPath := filepath.Join(taskDir, "spec.md")
	if err := os.WriteFile(specPath, []byte("# spec"), 0o600); err != nil {
		t.Fatal(err)
	}
	return codingagents.PlanRequest{
		TaskDir:                taskDir,
		FromFilePath:           specPath,
		Model:                  model,
		RequirementsOutputPath: filepath.Join(taskDir, "requirements.md"),
		PlanOutputPath:         filepath.Join(taskDir, "plan.md"),
		Interactive:            true,
	}
}

func TestSPA85CodexScopedHomeInheritsAuth(t *testing.T) {
	home := spa85FakeHome(t, "CODEX_HOME", ".codex")
	auth := []byte(`{"token":"codex-token"}`)
	config := []byte("model = 'gpt-5.5'\n")
	if err := os.WriteFile(
		filepath.Join(home, "auth.json"), auth, 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(home, "config.toml"), config, 0o600,
	); err != nil {
		t.Fatal(err)
	}

	taskDir := t.TempDir()
	authOut := filepath.Join(taskDir, "auth.out")
	configOut := filepath.Join(taskDir, "config.out")
	testutil.InstallPathScript(t, "codex", fmt.Sprintf(`#!/bin/sh
cat "$CODEX_HOME/auth.json" > %q
cat "$CODEX_HOME/config.toml" > %q
exit 0
`, authOut, configOut))

	if _, err := codex.New().Plan(
		t.Context(), spa85PlanReq(t, taskDir, "gpt-5.5"),
	); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := testutil.ReadTrimmedFile(t, authOut); got != string(auth) {
		t.Fatalf("auth = %q, want %q", got, auth)
	}
	wantConfig := string(config[:len(config)-1])
	if got := testutil.ReadTrimmedFile(t, configOut); got != wantConfig {
		t.Fatalf("config = %q, want %q", got, config)
	}
}

func TestSPA85DeepseekScopedHomeInheritsAuth(t *testing.T) {
	home := spa85FakeHome(t, "DEEPSEEK_HOME", ".deepseek")
	auth := []byte(`{"token":"deepseek-token"}`)
	config := []byte("model = 'deepseek-v4-pro'\n")
	if err := os.WriteFile(
		filepath.Join(home, "auth.json"), auth, 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(home, "config.toml"), config, 0o600,
	); err != nil {
		t.Fatal(err)
	}

	taskDir := t.TempDir()
	authOut := filepath.Join(taskDir, "auth.out")
	configOut := filepath.Join(taskDir, "config.out")
	testutil.InstallPathScript(t, "deepseek-tui", fmt.Sprintf(`#!/bin/sh
cat "$DEEPSEEK_HOME/auth.json" > %q
cat "$DEEPSEEK_HOME/config.toml" > %q
exit 0
`, authOut, configOut))

	if _, err := deepseek.New().Plan(
		t.Context(), spa85PlanReq(t, taskDir, "deepseek-v4-pro"),
	); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := testutil.ReadTrimmedFile(t, authOut); got != string(auth) {
		t.Fatalf("auth = %q, want %q", got, auth)
	}
	wantConfig := string(config[:len(config)-1])
	if got := testutil.ReadTrimmedFile(t, configOut); got != wantConfig {
		t.Fatalf("config = %q, want %q", got, config)
	}
}
