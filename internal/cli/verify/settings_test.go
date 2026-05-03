package verify

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// This file holds the verify orchestrator's settings-bucket
// wiring tests: lazy-open of `.j/settings`, the
// resolveInteractive precedence (explicit > stored > cobra
// default), and the fallback-to-prompt path when the settings DB
// is missing or corrupt. The pure agentpick.FromStore semantics
// live in internal/cli/agentpick/agentpick_test.go and are
// intentionally not re-asserted here.

// TestRun_FromStore_InteractivePrecedence collapses the four
// resolveInteractive precedence scenarios into a single table:
// stored-false-overrides-default, explicit-wins-over-stored,
// stored-unparseable-falls-to-default, and no-interactive-key.
func TestRun_FromStore_InteractivePrecedence(t *testing.T) {
	cases := []struct {
		name             string
		bucket           map[string]string
		explicit         *bool
		wantInteractive  bool
		wantNoStderrWarn bool
	}{
		{
			name:            "StoredFalseOverridesDefault",
			bucket:          map[string]string{"tool": "cursor", "model": "sonnet-4", "interactive": "false"},
			explicit:        nil,
			wantInteractive: false,
		},
		{
			name:            "ExplicitWinsOverStored",
			bucket:          map[string]string{"tool": "cursor", "model": "sonnet-4", "interactive": "false"},
			explicit:        boolPtr(true),
			wantInteractive: true,
		},
		{
			name:             "StoredUnparseableFallsToDefault",
			bucket:           map[string]string{"tool": "cursor", "model": "sonnet-4", "interactive": "garbage"},
			explicit:         nil,
			wantInteractive:  true,
			wantNoStderrWarn: true,
		},
		{
			name:            "NoInteractiveKeyKeepsDefault",
			bucket:          map[string]string{"tool": "cursor", "model": "sonnet-4"},
			explicit:        nil,
			wantInteractive: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t)
			for k, v := range tc.bucket {
				if err := s.Put(store.BucketVerifier, k, v); err != nil {
					t.Fatalf("Put(%s,%s): %v", k, v, err)
				}
			}
			id := seedWorkDoneTask(t, "x", "plan", "")
			agent := newScriptedAgent()
			agent.verifyVerdicts = []string{"PASS"}
			var stderr bytes.Buffer
			err := Run(context.Background(), Options{
				TaskID:      id,
				Stdin:       strings.NewReader(""),
				Stdout:      io.Discard,
				Stderr:      &stderr,
				Agents:      []codingagents.Agent{agent},
				UI:          &scriptedUI{},
				Store:       s,
				Interactive: tc.explicit,
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := agent.verifiedReqs[0].Interactive; got != tc.wantInteractive {
				t.Fatalf("Interactive = %v, want %v", got, tc.wantInteractive)
			}
			if tc.wantNoStderrWarn && strings.Contains(stderr.String(), "interactive") {
				t.Fatalf("stderr should not warn on unparseable interactive: %q", stderr.String())
			}
		})
	}
}

// TestRun_FromStore_NilStore_LazyOpenSucceeds drives the
// nil-Store + populated-settings branch of verifierFromStore:
// the helper opens `<cwd>/.j/settings`, reads the bucket, and
// surfaces the recorded tool/model so the UI prompts are skipped
// and storedVerifierInteractive returns the stored false.
func TestRun_FromStore_NilStore_LazyOpenSucceeds(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	settingsPath, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
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
	err = Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: &stderr,
		Agents: []codingagents.Agent{agent},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ui.toolCalls != 0 || ui.modelCalls != 0 {
		t.Fatalf("UI prompts should be skipped on lazy-open success: tool=%d model=%d", ui.toolCalls, ui.modelCalls)
	}
	if agent.verifiedReqs[0].Model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", agent.verifiedReqs[0].Model)
	}
	if agent.verifiedReqs[0].Interactive {
		t.Fatalf("Interactive = true, want false (stored)")
	}
}

// TestRun_FromStore_NilStore_SettingsOpenFails covers the lazy
// open-fails branches: with no caller-supplied Store and a
// `<cwd>/.j/settings` directory (instead of file) sabotaging
// bolt.Open, verifierFromStore and storedVerifierInteractive
// both surface the store.OpenSettings warning and fall back to
// the prompted Pick path.
func TestRun_FromStore_NilStore_SettingsOpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedWorkDoneTask(t, "x", "plan", "")
	settingsPath, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(settingsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	agent := newScriptedAgent()
	agent.verifyVerdicts = []string{"PASS"}
	var stderr bytes.Buffer
	err = Run(context.Background(), Options{
		TaskID: id,
		Stdout: io.Discard,
		Stderr: &stderr,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning: settings") {
		t.Fatalf("stderr should warn about settings open: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr should fall back to prompt: %q", stderr.String())
	}
}
