package testcases_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/util/agentlog"
)

// TestAgentLog_EmitTo_AppendsHumanReadable pins the EmitTo append-only
// invariant after the SPA-28 reshape: a second EmitTo against the
// same path must preserve every byte from the first, and both lines
// must be the new human-readable format (no `>>> J `).
//
// This guards the per-task `agent.log` invariant that planner→worker
// →verifier all share one log across phase boundaries.
func TestAgentLog_EmitTo_AppendsHumanReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	if err := os.WriteFile(
		path, []byte("prior transcript line\n"), 0o644,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := agentlog.EmitTo(
		path, "session_start", map[string]any{"task": "01KR"},
	); err != nil {
		t.Fatalf("first EmitTo: %v", err)
	}
	if err := agentlog.EmitTo(
		path, "child_exit", map[string]any{
			"name": "claude", "pid": 1234, "exit_code": 0,
		},
	); err != nil {
		t.Fatalf("second EmitTo: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	got := string(data)
	if !strings.HasPrefix(got, "prior transcript line\n") {
		t.Fatalf("prior bytes lost: %q", got)
	}
	if strings.Contains(got, ">>> J ") {
		t.Fatalf("legacy sentinel in log: %q", got)
	}
	if !strings.Contains(got, "session start") {
		t.Fatalf("missing session start marker: %q", got)
	}
	if !strings.Contains(got, "child exit") {
		t.Fatalf("missing child exit marker: %q", got)
	}
	if strings.Index(got, "session start") >
		strings.Index(got, "child exit") {
		t.Fatalf("expected chronological order: %q", got)
	}
}
