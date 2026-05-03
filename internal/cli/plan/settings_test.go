package plan

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

// This file holds the plan orchestrator's settings-bucket wiring
// tests: lazy-open of `.j/settings`, the resolveInteractive
// precedence (explicit > stored > cobra default), and the
// fallback-to-prompt path when the settings DB is missing or
// corrupt. The pure agentpick.FromStore semantics live in
// internal/cli/agentpick/agentpick_test.go and are intentionally
// not re-asserted here.

// TestRun_FromStore_InteractivePrecedence collapses the four
// resolveInteractive precedence scenarios into a single table:
// stored-false-overrides-default, explicit-wins-over-stored,
// stored-unparseable-falls-to-default, and no-interactive-key.
// Each row exercises the same Run path with a different bucket
// shape so failures pin to a specific branch via subtest name.
func TestRun_FromStore_InteractivePrecedence(t *testing.T) {
	cases := []struct {
		name               string
		bucket             map[string]string
		explicit           *bool
		wantInteractive    bool
		wantNoStderrWarn   bool
		wantPersistedValue string
	}{
		{
			name:               "StoredFalseOverridesDefault",
			bucket:             map[string]string{"tool": "cursor", "model": "sonnet-4", "interactive": "false"},
			explicit:           nil,
			wantInteractive:    false,
			wantPersistedValue: "false",
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
				if err := s.Put(store.BucketPlanner, k, v); err != nil {
					t.Fatalf("Put(%s,%s): %v", k, v, err)
				}
			}
			target := writeFromFile(t, "body")
			agent := newScriptedAgent()
			var stderr bytes.Buffer
			err := Run(context.Background(), Options{
				FromFile:    target,
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
			if agent.lastReq.Interactive != tc.wantInteractive {
				t.Fatalf("Interactive = %v, want %v", agent.lastReq.Interactive, tc.wantInteractive)
			}
			if tc.wantNoStderrWarn && strings.Contains(stderr.String(), "interactive") {
				t.Fatalf("stderr should not warn on unparseable interactive: %q", stderr.String())
			}
			if tc.wantPersistedValue != "" {
				if v, ok := mustGet(t, s, "interactive"); !ok || v != tc.wantPersistedValue {
					t.Fatalf("planner.interactive = %q (ok=%v), want %q", v, ok, tc.wantPersistedValue)
				}
			}
		})
	}
}

// TestRun_FromStore_EmptyStore_PromptsPick covers the lazy-open
// success branch of plannerFromStore + persistPlannerSelection:
// an empty project store on the markdown-import path triggers
// "Choose your favourite:", runs Pick, and writes the chosen
// agent back to `<cwd>/.j/settings`.
func TestRun_FromStore_EmptyStore_PromptsPick(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   &stderr,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
		Store:    nil,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr = %q, want choose-your-favourite line", stderr.String())
	}
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file not created: %v", err)
	}
}

// TestRun_FromStore_NilStore_SettingsOpenFails covers the lazy
// open-fails branches: with no caller-supplied Store and a
// `<cwd>/.j/settings` directory (instead of file) sabotaging
// bolt.Open, plannerFromStore and storedPlannerInteractive both
// surface the openSettingsStore warning and fall back to the
// prompted Pick path while still running the agent.
func TestRun_FromStore_NilStore_SettingsOpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
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
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	var stderr bytes.Buffer
	err = Run(context.Background(), Options{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   &stderr,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
		Store:    nil,
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
	if agent.planned != 1 {
		t.Fatalf("agent.Plan calls = %d, want 1", agent.planned)
	}
}
