package verifier

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// This file holds the verify orchestrator's settings-bucket wiring
// tests: lazy-open of `.j/settings`, stale interactive rows being
// ignored, and the fallback-to-prompt path when the settings DB is
// missing or corrupt. The pure resolver.AgentFromStore semantics live
// in internal/cli/picker/agent_test.go and are intentionally not
// re-asserted here.

// Interactive-flag precedence is pinned in
// internal/resolver/interactive_test.go. Run consumes the resolved
// bool directly.

// TestRun_FromStore_NilStore_LazyOpenSucceeds drives the
// nil-Store + populated-settings branch of resolver.Agent:
// the helper opens `<cwd>/.j/settings`, reads the bucket, and
// surfaces the recorded tool/model so the UI prompts are skipped.
// The stale interactive row must not override Options.Interactive.
func TestRun_FromStore_NilStore_LazyOpenSucceeds(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	settingsPath := store.DefaultPath()
	seed, err := store.Open(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Put(store.BucketVerifier, "tool", "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := seed.Put(store.BucketVerifier, "model", "gpt-5"); err != nil {
		t.Fatal(err)
	}
	if err := seed.Put(store.BucketVerifier, "interactive", "false"); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	id := seedWorkDoneTask(t, "x", "plan", "")
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
	ui := &scriptedUI{}
	var stderr bytes.Buffer
	err = Run(t.Context(), Options{
		TaskID:      id,
		Interactive: true,
		Stdout:      io.Discard,
		Stderr:      &stderr,
		Agents:      []codingagents.Agent{agent},
		UI:          ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.ToolCalls != 0 || ui.ModelCalls != 0 {
		t.Fatalf("UI prompts should be skipped on lazy-open success: tool=%d model=%d", ui.ToolCalls, ui.ModelCalls)
	}
	if agent.verifiedReqs[0].Model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", agent.verifiedReqs[0].Model)
	}
	if !agent.verifiedReqs[0].Interactive {
		t.Fatal("Interactive = false, want true from Options")
	}
}

// TestRun_FromStore_NilStore_SettingsOpenFails covers the lazy
// open-fails branches: with no caller-supplied Store and a
// `<cwd>/.j/settings` directory (instead of file) sabotaging
// bolt.Open, resolver.Agent and resolver.Interactive
// both surface the store.OpenSettings warning and fall back to
// the prompted Pick path.
func TestRun_FromStore_NilStore_SettingsOpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	settingsPath := store.DefaultPath()
	if err := os.Remove(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(settingsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
	var stderr bytes.Buffer
	err := Run(t.Context(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: &stderr,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "settings") {
		t.Fatalf("stderr should warn about settings open: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr should fall back to prompt: %q", stderr.String())
	}
}
