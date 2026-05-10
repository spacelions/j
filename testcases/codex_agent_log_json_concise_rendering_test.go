package testcases_test

// AC: Codex headless runs with --json produce concise agent.log
// markers (RFC3339Z + topic verb — k=v…), not raw JSONL. The
// test spawns a stub that emits a representative JSONL stream and
// confirms the log lines are human-readable markers with the
// expected content.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/codex"
	"github.com/spacelions/j/internal/testutil"
)

func TestCodexHeadlessJSONOutputAgentLogMarkers(t *testing.T) {
	lines := []map[string]any{
		{
			"type":      "thread.started",
			"thread_id": "t-001",
		},
		{
			"type": "turn.started",
		},
		{
			"type": "item.completed",
			"item": map[string]any{
				"id":   "it-msg",
				"type": "agent_message",
				"text": "file created successfully",
			},
		},
		{
			"type": "item.completed",
			"item": map[string]any{
				"id":   "it-cmd",
				"type": "command_execution",
				"command": "go test ./...",
				"status": "completed",
				"exit_code": 0,
				"aggregated_output": strings.Repeat("x", 500),
			},
		},
		{
			"type": "item.completed",
			"item": map[string]any{
				"id":   "it-fc",
				"type": "file_change",
				"status": "completed",
				"changes": []map[string]any{
					{"path": "a.go", "kind": "add"},
				},
			},
		},
		{
			"type": "turn.completed",
			"usage": map[string]any{
				"input_tokens":           120,
				"cached_input_tokens":    30,
				"output_tokens":          45,
				"reasoning_output_tokens": 12,
			},
		},
		{
			"type":   "turn.failed",
			"error": map[string]any{"message": "tool timeout"},
		},
		{
			"type":    "error",
			"message": "process crash",
		},
		{
			"type": "item.completed",
			"item": map[string]any{
				"id":   "it-reason",
				"type": "reasoning",
				"text": "thinking about next step",
			},
		},
		{
			"type": "item.completed",
			"item": map[string]any{
				"id":   "it-web",
				"type": "web_search",
				"query": "golang sync.Map",
				"action": map[string]any{"type": "search"},
			},
		},
		{
			"type": "item.updated",
			"item": map[string]any{
				"id":   "it-todo",
				"type": "todo_list",
				"items": []map[string]any{
					{"text": "write test", "completed": true},
					{"text": "run test", "completed": true},
					{"text": "commit", "completed": false},
				},
			},
		},
	}

	var raw strings.Builder
	for _, l := range lines {
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		raw.Write(b)
		raw.WriteByte('\n')
	}

	_ = testutil.InstallExecutableStub(t, testutil.ExecutableStubOptions{
		Binary:   "codex",
		Stdout:   raw.String(),
		ExitCode: 0,
	})

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("# spec"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "agent.log")

	req := codingagents.PlanRequest{
		TaskDir:                dir,
		FromFilePath:           specPath,
		Model:                  "gpt-5.5",
		RequirementsOutputPath: filepath.Join(dir, "reqs.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            false,
		AgentLogPath:           logPath,
	}
	pid, err := codex.New().Plan(t.Context(), req)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("headless Plan pid = %d, want > 0", pid)
	}

	got := testutil.WaitForLog(t, logPath, "agent thread", 5*time.Second)

	// No raw JSONL should appear in agent.log.
	for _, rawKey := range []string{
		`"type":"thread.started"`,
		`"type":"item.completed"`,
		`"aggregated_output"`,
		`"exit_code"`,
	} {
		if strings.Contains(got, rawKey) {
			t.Errorf("raw JSON key %q leaked into agent.log: %s",
				rawKey, got)
		}
	}

	// Essential content verifications.
	wants := []string{
		"agent thread",
		"thread_id=t-001",
		"agent status",
		"status=turn_started",
		"agent message",
		"text=file created successfully",
		"agent command",
		"command=go test ./...",
		"exit_code=0",
		"output_bytes=500",
		"agent file_change",
		"files=add:a.go",
		"agent result",
		"input_tokens=120",
		"cached_input_tokens=30",
		"output_tokens=45",
		"reasoning_output_tokens=12",
		"agent error",
		"message=tool timeout",
		"message=process crash",
		"agent thinking",
		"text=thinking about next step",
		"agent web_search",
		"query=golang sync.Map",
		"action=search",
		"agent todo_list",
		"items=3",
		"completed=2",
		"pending=1",
		"current=commit",
		"phase=completed",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("agent.log missing %q: %s", w, got)
		}
	}

	// Command output must not leak.
	if strings.Contains(got, strings.Repeat("x", 500)) {
		t.Errorf("aggregated command output leaked into agent.log")
	}
}
