package testcases_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/codex"
)

// TestCodexResumeIDCaptureRoundTrip pins the load-bearing resume
// continuity contract: after a codex run leaves a rollout file
// behind in <taskDir>/.codex-home/sessions/YYYY/MM/DD/, the
// orchestrator-facing codingagents.CaptureResumeID dispatches into
// the codex backend (which satisfies ResumeIDCapturer) and returns
// the matching thread id so a subsequent resume run can thread it
// via `exec resume <id>`.
//
// Black-box: drive the package-level CaptureResumeID free function
// the same way the planner / worker / verifier wiring does, with a
// fixture rollout JSONL written to a fake task-scoped home.
func TestCodexResumeIDCaptureRoundTrip(t *testing.T) {
	taskDir := t.TempDir()
	rolloutDir := filepath.Join(
		taskDir, ".codex-home", "sessions", "2026", "05", "10",
	)
	if err := os.MkdirAll(rolloutDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	workspace := t.TempDir()
	since := time.Now().Add(-1 * time.Hour)
	createdAt := since.Add(10 * time.Minute)
	wantID := "019e0d41-54c4-79f0-aaca-23bfa44894d0"

	envelope := map[string]any{
		"timestamp": createdAt.UTC().Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload": map[string]any{
			"id":        wantID,
			"cwd":       workspace,
			"timestamp": createdAt.UTC().Format(time.RFC3339Nano),
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	rolloutPath := filepath.Join(
		rolloutDir, "rollout-2026-05-10T15-00-45-"+wantID+".jsonl",
	)
	body := make([]byte, 0, len(data)+1)
	body = append(body, data...)
	body = append(body, '\n')
	if err := os.WriteFile(rolloutPath, body, 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	agent := codex.New()
	got, err := codingagents.CaptureResumeID(
		t.Context(), agent, taskDir, since,
	)
	if err != nil {
		t.Fatalf("CaptureResumeID: %v", err)
	}
	if got != wantID {
		t.Fatalf("CaptureResumeID = %q, want %q", got, wantID)
	}
}

// TestCodexResumeIDCaptureMissingStoreNoError pins the resilience
// branch: when the user has no codex session store yet (fresh
// machine, or they cleared it), the orchestrator-facing
// CaptureResumeID returns ("", nil) so the orchestrator starts a
// fresh session rather than failing the command.
func TestCodexResumeIDCaptureMissingStoreNoError(t *testing.T) {
	taskDir := t.TempDir() // no sessions/ subdir

	agent := codex.New()
	got, err := codingagents.CaptureResumeID(
		t.Context(), agent, taskDir, time.Now().Add(-time.Hour),
	)
	if err != nil {
		t.Fatalf("CaptureResumeID: %v", err)
	}
	if got != "" {
		t.Fatalf("CaptureResumeID = %q, want empty", got)
	}
}
