//go:build !windows

package testcases_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
)

// installStreamJSONStub writes a shell stub at $tmp/<binary> that
// emits one assistant text JSON event on stdout, then exits zero.
// PATH is prepended with the temp dir so codingagents resolves the
// stub. The stub is enough to drive a happy-path run end-to-end
// while keeping the test hermetic.
func installStreamJSONStub(
	t *testing.T, binary, jsonLine string,
) {
	t.Helper()
	dir := t.TempDir()
	body := fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s\\n' '%s'\nexit 0\n",
		strings.ReplaceAll(jsonLine, "'", "'\\''"),
	)
	bin := filepath.Join(dir, binary)
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(
		"PATH",
		dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func waitForLogContains(
	t *testing.T, logPath string, needles ...string,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(logPath)
		body := string(data)
		ok := true
		for _, n := range needles {
			if !strings.Contains(body, n) {
				ok = false
				break
			}
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"timeout waiting for %v in %s; last=%q",
				needles, logPath, body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAgentLogCaptures_ClaudeReasoning end-to-ends the SPA-68 fix
// for the Claude backend: the headless Plan path must turn the
// CLI's stream-json output into human-readable `text:`-style lines
// in the per-task agent.log instead of dropping everything but the
// final assistant message. We drive a stub `claude` that prints one
// JSON event and assert the formatted line lands in the log.
func TestAgentLogCaptures_ClaudeReasoning(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(
		target, []byte("# spec\nbody"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "agent.log")
	installStreamJSONStub(t, "claude",
		`{"type":"assistant","message":{"content":`+
			`[{"type":"text","text":"hello-claude"}]}}`,
	)
	pid, err := claude.New().Plan(
		t.Context(), codingagents.PlanRequest{
			FromFilePath:           target,
			Model:                  "sonnet",
			RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
			PlanOutputPath:         filepath.Join(dir, "plan.md"),
			Interactive:            false,
			AgentLogPath:           logPath,
		},
	)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d", pid)
	}
	waitForLogContains(t, logPath,
		"text: hello-claude", "child exit")
}

// TestAgentLogCaptures_CursorReasoning is the cursor-agent
// counterpart: the headless Plan path must surface stream-json
// events in agent.log via agentlog.CursorStream(). The stub stands
// in for the real cursor-agent binary.
func TestAgentLogCaptures_CursorReasoning(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(
		target, []byte("# spec\nbody"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "agent.log")
	installStreamJSONStub(t, "cursor-agent",
		`{"type":"assistant","message":{"content":`+
			`[{"type":"text","text":"hello-cursor"}]}}`,
	)
	pid, err := cursor.New().Plan(
		t.Context(), codingagents.PlanRequest{
			FromFilePath:           target,
			Model:                  "sonnet",
			RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
			PlanOutputPath:         filepath.Join(dir, "plan.md"),
			Interactive:            false,
			AgentLogPath:           logPath,
		},
	)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d", pid)
	}
	waitForLogContains(t, logPath,
		"text: hello-cursor", "child exit")
}
