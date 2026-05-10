package testcases_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/deepseek"
)

// TestDeepseekCaptureIgnoresSessionWorkspace verifies AC #4 for the
// deepseek backend: capture succeeds even when the session file records
// a workspace that differs from the workspace j used. The new per-task
// scoped-home implementation anchors the session store to
// <taskDir>/.deepseek-home, so the session workspace field is
// irrelevant to the lookup.
func TestDeepseekCaptureIgnoresSessionWorkspace(t *testing.T) {
	taskDir := t.TempDir()
	sessionsDir := filepath.Join(taskDir, ".deepseek-home", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	since := time.Now().Add(-1 * time.Hour)
	createdAt := since.Add(10 * time.Minute)
	wantID := "ds-cwd-mismatch-id"

	// workspace field differs from the one j passed — must not block
	// capture under the new per-task scoped-home implementation.
	envelope := map[string]any{
		"metadata": map[string]any{
			"id":         wantID,
			"workspace":  "/private/var/folders/totally/different",
			"created_at": createdAt.UTC().Format(time.RFC3339Nano),
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	sessionPath := filepath.Join(sessionsDir, wantID+".json")
	if err := os.WriteFile(sessionPath, data, 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	agent := deepseek.New()
	got, err := codingagents.CaptureResumeID(
		t.Context(), agent, taskDir, since,
	)
	if err != nil {
		t.Fatalf("CaptureResumeID: %v", err)
	}
	if got != wantID {
		t.Fatalf(
			"CaptureResumeID = %q, want %q; "+
				"workspace mismatch must not prevent capture",
			got, wantID,
		)
	}
}
