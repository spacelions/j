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

// TestConcurrentCodexTasksNoCrossCaptureAC3 verifies AC #3 for codex:
// two j tasks running concurrently against the same project workspace
// each only see the sessions produced under their own per-task
// scoped home, never the sessions from the other task's run.
func TestConcurrentCodexTasksNoCrossCaptureAC3(t *testing.T) {
	taskA := t.TempDir()
	taskB := t.TempDir()
	since := time.Now().Add(-time.Hour)
	ts := since.Add(10 * time.Minute)

	writeCodexRollout(t, taskA, "id-a", ts)
	writeCodexRollout(t, taskB, "id-b", ts.Add(time.Minute))

	agent := codex.New()
	gotA, err := codingagents.CaptureResumeID(
		t.Context(), agent, taskA, since,
	)
	if err != nil {
		t.Fatalf("CaptureResumeID taskA: %v", err)
	}
	if gotA != "id-a" {
		t.Fatalf("taskA captured %q, want id-a", gotA)
	}

	gotB, err := codingagents.CaptureResumeID(
		t.Context(), agent, taskB, since,
	)
	if err != nil {
		t.Fatalf("CaptureResumeID taskB: %v", err)
	}
	if gotB != "id-b" {
		t.Fatalf("taskB captured %q, want id-b", gotB)
	}
}

// TestConcurrentDeepseekTasksNoCrossCaptureAC3 mirrors
// TestConcurrentCodexTasksNoCrossCaptureAC3 for the deepseek backend.
func TestConcurrentDeepseekTasksNoCrossCaptureAC3(t *testing.T) {
	taskA := t.TempDir()
	taskB := t.TempDir()
	since := time.Now().Add(-time.Hour)
	ts := since.Add(10 * time.Minute)

	writeDeepseekSession(t, taskA, "ds-id-a", ts)
	writeDeepseekSession(t, taskB, "ds-id-b", ts.Add(time.Minute))

	agent := deepseek.New()
	gotA, err := codingagents.CaptureResumeID(
		t.Context(), agent, taskA, since,
	)
	if err != nil {
		t.Fatalf("CaptureResumeID taskA: %v", err)
	}
	if gotA != "ds-id-a" {
		t.Fatalf("taskA captured %q, want ds-id-a", gotA)
	}

	gotB, err := codingagents.CaptureResumeID(
		t.Context(), agent, taskB, since,
	)
	if err != nil {
		t.Fatalf("CaptureResumeID taskB: %v", err)
	}
	if gotB != "ds-id-b" {
		t.Fatalf("taskB captured %q, want ds-id-b", gotB)
	}
}

func writeCodexRollout(
	t *testing.T, taskDir, id string, ts time.Time,
) {
	t.Helper()
	dir := filepath.Join(
		taskDir, ".codex-home", "sessions", "2026", "05", "10",
	)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := map[string]any{
		"timestamp": ts.UTC().Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload": map[string]any{
			"id":        id,
			"cwd":       taskDir,
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

func writeDeepseekSession(
	t *testing.T, taskDir, id string, ts time.Time,
) {
	t.Helper()
	dir := filepath.Join(taskDir, ".deepseek-home", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := map[string]any{
		"metadata": map[string]any{
			"id":         id,
			"workspace":  taskDir,
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
