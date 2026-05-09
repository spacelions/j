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

// TestDeepseekResumeIDCaptureRoundTrip pins the load-bearing resume
// continuity contract: after a deepseek run leaves a session file
// behind in $DEEPSEEK_HOME/sessions/, the orchestrator-facing
// codingagents.CaptureResumeID dispatches into the deepseek backend
// (which satisfies ResumeIDCapturer) and returns the matching session
// id so a subsequent resume run can thread it via -r <id>.
//
// Black-box: drive the package-level CaptureResumeID free function
// the same way the planner / worker / verifier wiring does, with a
// fixture session JSON written to a fake DEEPSEEK_HOME.
func TestDeepseekResumeIDCaptureRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DEEPSEEK_HOME", home)
	sessionsDir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	workspace := t.TempDir()
	since := time.Now().Add(-1 * time.Hour)
	createdAt := since.Add(10 * time.Minute)
	wantID := "session-uuid-aaaa-bbbb-cccc"

	envelope := map[string]any{
		"metadata": map[string]any{
			"id":         wantID,
			"workspace":  workspace,
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
		t.Context(), agent, workspace, since,
	)
	if err != nil {
		t.Fatalf("CaptureResumeID: %v", err)
	}
	if got != wantID {
		t.Fatalf("CaptureResumeID = %q, want %q", got, wantID)
	}
}

// TestDeepseekResumeIDCaptureMissingStoreNoError pins the resilience
// branch of the load-bearing requirement: when the user has no
// DeepSeek session store yet (fresh machine, or they cleared it), the
// orchestrator-facing CaptureResumeID returns ("", nil) so the
// orchestrator starts a fresh session rather than failing the
// command. Acceptance criteria: "If the original session can no
// longer be located ... the resume run starts a fresh session rather
// than failing the command."
func TestDeepseekResumeIDCaptureMissingStoreNoError(t *testing.T) {
	home := t.TempDir() // no sessions/ subdir
	t.Setenv("DEEPSEEK_HOME", home)

	agent := deepseek.New()
	got, err := codingagents.CaptureResumeID(
		t.Context(), agent, "/some/workspace", time.Now().Add(-time.Hour),
	)
	if err != nil {
		t.Fatalf("CaptureResumeID: %v", err)
	}
	if got != "" {
		t.Fatalf("CaptureResumeID = %q, want empty", got)
	}
}
