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

// TestAgentLog_Claude_StreamJSON_LandsInLog drives the SPA-68
// acceptance criterion for the claude backend: a headless Plan run
// passes `--output-format stream-json --verbose
// --include-partial-messages`, and the JSON event lines the CLI
// prints land verbatim in the per-task agent.log (interleaved with
// the existing lifecycle markers). The test installs a fake `claude`
// on PATH that prints three canned stream-JSON events and exits;
// the orchestrator-style assertion grep below the timeout catches
// the regression where headless mode would otherwise collapse
// everything down to the final assistant text only.
func TestAgentLog_Claude_StreamJSON_LandsInLog(t *testing.T) {
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
		`"type":"system"`,
		`"thinking":"hmm"`,
		`"text":"hi"`,
		`"type":"result"`,
	}
	body := waitForAll(t, logPath, want)
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Fatalf("agent.log missing %q: %q", w, body)
		}
	}
}

// TestAgentLog_Cursor_StreamJSON_LandsInLog is the cursor-agent
// counterpart: headless Plan must pass `--output-format stream-json
// --stream-partial-output` and the emitted JSON events must reach
// agent.log unmodified.
func TestAgentLog_Cursor_StreamJSON_LandsInLog(t *testing.T) {
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
		`"type":"system"`,
		`"text":"ok"`,
		`"type":"result"`,
	}
	body := waitForAll(t, logPath, want)
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Fatalf("agent.log missing %q: %q", w, body)
		}
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
