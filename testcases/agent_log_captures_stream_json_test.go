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

// TestAgentLog_Claude_FormattedMarkers_LandInLog drives the SPA-73
// acceptance criterion for the claude backend: a headless Plan run
// passes `--output-format stream-json --verbose
// --dangerously-skip-permissions` (no `--include-partial-messages`),
// and each stream-json event the CLI prints lands in agent.log as a
// human-readable agentlog marker line — not as raw JSON. The test
// installs a fake `claude` on PATH that prints three canned events
// and asserts the formatter's marker headers reach the log.
func TestAgentLog_Claude_FormattedMarkers_LandInLog(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "agent.log")
	installStreamJSONStub(t, claude.Binary,
		`{"type":"system","subtype":"init","model":"sonnet","tools":["X"]}`,
		`{"type":"assistant","message":{"content":[`+
			`{"type":"thinking","thinking":"hmm"},`+
			`{"type":"text","text":"hi"}]}}`,
		`{"type":"result","subtype":"success","stop_reason":"end_turn"}`,
	)

	pid, err := claude.New().Plan(t.Context(), codingagents.PlanRequest{
		FromFilePath:           target,
		Model:                  "sonnet",
		RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            false,
		AgentLogPath:           logPath,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("Plan pid = %d, want > 0", pid)
	}
	want := []string{
		"agent init",
		"agent thinking",
		"agent message",
		"agent result",
	}
	body := waitForAll(t, logPath, want)
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Fatalf("agent.log missing %q: %q", w, body)
		}
	}
	// SPA-73: no raw stream-json envelopes survive in the log.
	if strings.Contains(body, `"type":"system"`) ||
		strings.Contains(body, `"type":"assistant"`) ||
		strings.Contains(body, `"type":"result"`) {
		t.Fatalf("raw JSON leaked into agent.log: %q", body)
	}
}

// TestAgentLog_Cursor_FormattedMarkers_LandInLog is the cursor-agent
// counterpart: headless Plan must pass `--output-format stream-json`
// (no `--stream-partial-output`) and the emitted events must reach
// agent.log as agentlog marker lines, not raw JSON.
func TestAgentLog_Cursor_FormattedMarkers_LandInLog(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("# task"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "agent.log")
	installStreamJSONStub(t, cursor.Binary,
		`{"type":"system","subtype":"init","model":"composer-2"}`,
		`{"type":"assistant","message":{"content":[`+
			`{"type":"text","text":"ok"}]}}`,
		`{"type":"result","subtype":"success","result":"ok"}`,
	)

	pid, err := cursor.New().Plan(t.Context(), codingagents.PlanRequest{
		FromFilePath:           target,
		Model:                  "composer-2",
		RequirementsOutputPath: filepath.Join(dir, "requirements.md"),
		PlanOutputPath:         filepath.Join(dir, "plan.md"),
		Interactive:            false,
		AgentLogPath:           logPath,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("Plan pid = %d, want > 0", pid)
	}
	want := []string{
		"agent init",
		"agent message",
		"agent result",
	}
	body := waitForAll(t, logPath, want)
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Fatalf("agent.log missing %q: %q", w, body)
		}
	}
	if strings.Contains(body, `"type":"system"`) ||
		strings.Contains(body, `"type":"assistant"`) ||
		strings.Contains(body, `"type":"result"`) {
		t.Fatalf("raw JSON leaked into agent.log: %q", body)
	}
}

// installStreamJSONStub writes a shell script named binary into a
// fresh dir, prepends it to PATH, and primes it to print every line
// in events to stdout before exiting 0. It mirrors the pattern used
// by the per-backend posix tests but lives here so this acceptance
// test can drive both backends without reaching into their packages.
func installStreamJSONStub(t *testing.T, binary string, events ...string) {
	t.Helper()
	dir := t.TempDir()
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	for _, ev := range events {
		fmt.Fprintf(&sb, "printf '%%s\\n' %q\n", ev)
	}
	sb.WriteString("exit 0\n")
	bin := filepath.Join(dir, binary)
	if err := os.WriteFile(bin, []byte(sb.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// waitForAll polls logPath until every needle in want is present or
// a 5s deadline elapses. Returns the final file contents so the
// caller can include them in any failure message.
func waitForAll(t *testing.T, logPath string, want []string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(logPath)
		body := string(data)
		hit := 0
		for _, w := range want {
			if strings.Contains(body, w) {
				hit++
			}
		}
		if hit == len(want) {
			return body
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %v in %s; got %q",
				want, logPath, body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
