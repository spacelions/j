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

// TestCodexCaptureIgnoresRolloutCWD verifies AC #4: capture succeeds
// even when the rollout file records a cwd that differs from the path
// j used for the workspace (e.g. /private/var vs /var on macOS, or a
// symlink-resolved path). The new per-task scoped-home implementation
// anchors the session store to <taskDir>/.codex-home, so the rollout
// cwd field is irrelevant to the lookup.
func TestCodexCaptureIgnoresRolloutCWD(t *testing.T) {
	taskDir := t.TempDir()
	rolloutDir := filepath.Join(
		taskDir, ".codex-home", "sessions", "2026", "05", "10",
	)
	if err := os.MkdirAll(rolloutDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	since := time.Now().Add(-1 * time.Hour)
	createdAt := since.Add(10 * time.Minute)
	wantID := "cwd-mismatch-session-id"

	// Deliberately use a cwd value that does NOT match the workspace j
	// would pass. Before the fix, this would prevent capture. After the
	// fix, the cwd field is not consulted.
	envelope := map[string]any{
		"timestamp": createdAt.UTC().Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload": map[string]any{
			"id":  wantID,
			"cwd": "/private/var/folders/totally/different/path",
			"timestamp": createdAt.UTC().Format(
				time.RFC3339Nano,
			),
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	data = append(data, '\n')
	rolloutPath := filepath.Join(rolloutDir, "rollout-mismatch.jsonl")
	if err := os.WriteFile(rolloutPath, data, 0o644); err != nil {
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
		t.Fatalf(
			"CaptureResumeID = %q, want %q; "+
				"cwd mismatch must not prevent capture",
			got, wantID,
		)
	}
}
