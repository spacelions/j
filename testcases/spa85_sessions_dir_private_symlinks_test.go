package testcases_test

// spa85_sessions_dir_private_symlinks_test.go
//
// Acceptance criterion: the per-task scoped home exposes the user's real
// auth/config files via symlinks, but keeps sessions/ as a private
// directory (never a symlink). This is the structural invariant that
// simultaneously satisfies AC-7 (auth visible) and AC-3/5/6 (sessions
// isolated per task, stale excluded, fork safe).
//
// Black-box: drive Agent.Plan against a stub binary that exits 0 without
// touching the filesystem. Inspect the resulting per-task home structure.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/coding-agents/codex"
	"github.com/spacelions/j/internal/coding-agents/deepseek"
	"github.com/spacelions/j/internal/testutil"
)

func TestSPA85CodexScopedHomeSessionsDirIsPrivate(t *testing.T) {
	home := spa85FakeHome(t, "CODEX_HOME", ".codex")
	if err := os.WriteFile(
		filepath.Join(home, "auth.json"), []byte("{}"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(home, "config.toml"), []byte(""), 0o600,
	); err != nil {
		t.Fatal(err)
	}

	taskDir := t.TempDir()
	testutil.InstallPathScript(t, "codex", "#!/bin/sh\nexit 0\n")

	if _, err := codex.New().Plan(
		t.Context(), spa85PlanReq(t, taskDir, "gpt-5.5"),
	); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	scopedHome := filepath.Join(taskDir, ".codex-home")

	// auth.json must exist and be a symlink (inherited from real home).
	authInfo, err := os.Lstat(filepath.Join(scopedHome, "auth.json"))
	if err != nil {
		t.Fatalf("stat auth.json: %v", err)
	}
	if authInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatal("auth.json should be a symlink in the scoped home")
	}

	// sessions/ must exist and must NOT be a symlink (it is a private dir).
	sessInfo, err := os.Lstat(filepath.Join(scopedHome, "sessions"))
	if err != nil {
		t.Fatalf("stat sessions/: %v", err)
	}
	if sessInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatal("sessions/ must be a private directory, not a symlink")
	}
	if !sessInfo.IsDir() {
		t.Fatal("sessions/ must be a directory")
	}
}

func TestSPA85DeepseekScopedHomeSessionsDirIsPrivate(t *testing.T) {
	home := spa85FakeHome(t, "DEEPSEEK_HOME", ".deepseek")
	if err := os.WriteFile(
		filepath.Join(home, "auth.json"), []byte("{}"), 0o600,
	); err != nil {
		t.Fatal(err)
	}

	taskDir := t.TempDir()
	testutil.InstallPathScript(t, "deepseek-tui", "#!/bin/sh\nexit 0\n")

	if _, err := deepseek.New().Plan(
		t.Context(), spa85PlanReq(t, taskDir, "deepseek-v4-pro"),
	); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	scopedHome := filepath.Join(taskDir, ".deepseek-home")

	authInfo, err := os.Lstat(filepath.Join(scopedHome, "auth.json"))
	if err != nil {
		t.Fatalf("stat auth.json: %v", err)
	}
	if authInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatal("auth.json should be a symlink in the scoped home")
	}

	sessInfo, err := os.Lstat(filepath.Join(scopedHome, "sessions"))
	if err != nil {
		t.Fatalf("stat sessions/: %v", err)
	}
	if sessInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatal("sessions/ must be a private directory, not a symlink")
	}
	if !sessInfo.IsDir() {
		t.Fatal("sessions/ must be a directory")
	}
}
