package testcases_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/codex"
	"github.com/spacelions/j/internal/coding-agents/deepseek"
)

// TestCodexStaleInTaskSessionExcludedBySince verifies AC #5 for codex:
// a rollout written by an earlier phase run of the same task (before
// the current phase's begin-time) is excluded by the since filter so
// it never shadows the fresh session.
func TestCodexStaleInTaskSessionExcludedBySince(t *testing.T) {
	taskDir := t.TempDir()
	dir := filepath.Join(
		taskDir, ".codex-home", "sessions", "2026", "05", "10",
	)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	phaseBegin := time.Now().Add(-30 * time.Minute)

	// Stale rollout from a previous phase of this task — timestamp is
	// before the current phase's begin-time.
	staleTS := phaseBegin.Add(-10 * time.Minute)
	writeCodexRolloutAt(t, dir, "stale-id", staleTS)

	// Fresh rollout from the current phase run.
	freshTS := phaseBegin.Add(5 * time.Minute)
	writeCodexRolloutAt(t, dir, "fresh-id", freshTS)

	agent := codex.New()
	got, err := codingagents.CaptureResumeID(
		t.Context(), agent, taskDir, phaseBegin,
	)
	if err != nil {
		t.Fatalf("CaptureResumeID: %v", err)
	}
	if got != "fresh-id" {
		t.Fatalf(
			"CaptureResumeID = %q, want fresh-id; "+
				"stale in-task session must be excluded by since",
			got,
		)
	}
}

// TestDeepseekStaleInTaskSessionExcludedBySince mirrors the codex
// test for the deepseek backend.
func TestDeepseekStaleInTaskSessionExcludedBySince(t *testing.T) {
	taskDir := t.TempDir()
	dir := filepath.Join(taskDir, ".deepseek-home", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	phaseBegin := time.Now().Add(-30 * time.Minute)
	staleTS := phaseBegin.Add(-10 * time.Minute)
	freshTS := phaseBegin.Add(5 * time.Minute)

	writeDeepseekSessionAt(t, dir, "ds-stale-id", staleTS)
	writeDeepseekSessionAt(t, dir, "ds-fresh-id", freshTS)

	agent := deepseek.New()
	got, err := codingagents.CaptureResumeID(
		t.Context(), agent, taskDir, phaseBegin,
	)
	if err != nil {
		t.Fatalf("CaptureResumeID: %v", err)
	}
	if got != "ds-fresh-id" {
		t.Fatalf(
			"CaptureResumeID = %q, want ds-fresh-id; "+
				"stale in-task session must be excluded by since",
			got,
		)
	}
}

func writeCodexRolloutAt(
	t *testing.T, dir, id string, ts time.Time,
) {
	t.Helper()
	env := map[string]any{
		"timestamp": ts.UTC().Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload": map[string]any{
			"id":        id,
			"cwd":       dir,
			"timestamp": ts.UTC().Format(time.RFC3339Nano),
		},
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, "rollout-"+id+".jsonl")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeDeepseekSessionAt(
	t *testing.T, dir, id string, ts time.Time,
) {
	t.Helper()
	env := map[string]any{
		"metadata": map[string]any{
			"id":         id,
			"workspace":  dir,
			"created_at": ts.UTC().Format(time.RFC3339Nano),
		},
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, id+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
