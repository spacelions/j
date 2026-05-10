package testcases_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/spacelions/j/internal/coding-agents/codex"
	"github.com/spacelions/j/internal/coding-agents/deepseek"
	"github.com/spacelions/j/internal/testutil"
)

func spa85WriteLegacyCodexRollout(
	t *testing.T, home, rel, id string,
) string {
	t.Helper()
	path := filepath.Join(home, "sessions", rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"type": "session_meta",
		"payload": map[string]any{
			"id":        id,
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal rollout: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func spa85WriteLegacyDeepseekSession(
	t *testing.T, home, name, id string,
) string {
	t.Helper()
	path := filepath.Join(home, "sessions", name+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"metadata": map[string]any{
			"id":         id,
			"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSPA85CodexLegacyResumeImportsRollout(t *testing.T) {
	const resumeID = "019e0d41-54c4-79f0-aaca-23bfa44894d0"
	home := spa85FakeHome(t, "CODEX_HOME", ".codex")
	rel := "2026-05-09/rollout-2026-05-09T15-00-45-" +
		resumeID + ".jsonl"
	src := spa85WriteLegacyCodexRollout(t, home, rel, resumeID)
	stub := testutil.InstallExecutableStub(
		t,
		testutil.ExecutableStubOptions{
			Binary:   "codex",
			Stdout:   "ok\n",
			ExitCode: 0,
		},
	)
	taskDir := t.TempDir()
	req := spa85PlanReq(t, taskDir, "gpt-5.5")
	req.Interactive = false
	req.ResumeChatID = resumeID
	req.AgentLogPath = filepath.Join(taskDir, "agent.log")

	if _, err := codex.New().Plan(t.Context(), req); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	argv := testutil.WaitForNullArgs(t, stub.CallsPath, 8, 5*time.Second)
	if !slices.Contains(argv, "exec") || !slices.Contains(argv, "resume") {
		t.Fatalf("argv does not resume through exec: %v", argv)
	}
	if !slices.Contains(argv, resumeID) {
		t.Fatalf("argv missing resume id %q: %v", resumeID, argv)
	}
	target := filepath.Join(taskDir, ".codex-home", "sessions", rel)
	if got, err := os.Readlink(target); err != nil || got != src {
		t.Fatalf("legacy symlink = %q, %v; want %q", got, err, src)
	}
}

func TestSPA85DeepseekLegacyResumeImportsSession(t *testing.T) {
	const resumeID = "deepseek-resume-id"
	home := spa85FakeHome(t, "DEEPSEEK_HOME", ".deepseek")
	src := spa85WriteLegacyDeepseekSession(t, home, "legacy", resumeID)
	stub := testutil.InstallExecutableStub(
		t,
		testutil.ExecutableStubOptions{
			Binary:   "deepseek-tui",
			Stdout:   "ok\n",
			ExitCode: 0,
		},
	)
	taskDir := t.TempDir()
	req := spa85PlanReq(t, taskDir, "deepseek-v4-pro")
	req.Interactive = false
	req.ResumeChatID = resumeID
	req.AgentLogPath = filepath.Join(taskDir, "agent.log")

	if _, err := deepseek.New().Plan(t.Context(), req); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	argv := testutil.WaitForNullArgs(t, stub.CallsPath, 9, 5*time.Second)
	if !slices.Contains(argv, "-r") || !slices.Contains(argv, resumeID) {
		t.Fatalf("argv does not include deepseek resume id: %v", argv)
	}
	target := filepath.Join(
		taskDir, ".deepseek-home", "sessions", "legacy.json",
	)
	if got, err := os.Readlink(target); err != nil || got != src {
		t.Fatalf("legacy symlink = %q, %v; want %q", got, err, src)
	}
}
