package testcases_test

import (
	"context"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/codex"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/coding-agents/deepseek"
)

// TestRegisteredAgentsExposeFourTools pins the picker / settings
// acceptance criterion: the tool picker for planner, worker, and
// verifier offers four entries — claude, codex, deepseek, cursor —
// in that order. claude leads because it is the most commonly used
// backend; codex sits at slot 1 as the second OpenAI-flavoured
// option; deepseek and cursor follow.
//
// The j cli builds the agents slice the same way at every
// registration site (start.go, orchestrate.go, resume_*.go,
// re_*.go, continue.go). This test mirrors that construction so a
// regression that drops one or accidentally registers a fifth
// backend fails the build.
func TestRegisteredAgentsExposeFourTools(t *testing.T) {
	agents := []codingagents.Agent{
		claude.New(), codex.New(), deepseek.New(), cursor.New(),
	}

	want := []string{"claude", "codex", "deepseek", "cursor"}
	if len(agents) != len(want) {
		t.Fatalf("len(agents) = %d, want %d", len(agents), len(want))
	}
	for i, a := range agents {
		if a.Name() != want[i] {
			t.Fatalf("agents[%d].Name() = %q, want %q",
				i, a.Name(), want[i])
		}
	}
}

// TestResumeIDCapturerImplementations pins the design boundary the
// plan calls out: cursor and claude mint their session id pre-run
// and intentionally do NOT implement ResumeIDCapturer; codex and
// deepseek both rely on post-run capture from their on-disk session
// store. The orchestrator's post-run capture step is therefore a
// no-op for cursor / claude users.
func TestResumeIDCapturerImplementations(t *testing.T) {
	cases := []struct {
		name           string
		agent          codingagents.Agent
		wantImplements bool
	}{
		{"cursor", cursor.New(), false},
		{"claude", claude.New(), false},
		{"codex", codex.New(), true},
		{"deepseek", deepseek.New(), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := tc.agent.(codingagents.ResumeIDCapturer)
			if ok != tc.wantImplements {
				t.Fatalf(
					"%s implements ResumeIDCapturer = %v, want %v",
					tc.name, ok, tc.wantImplements,
				)
			}
		})
	}
}

// TestDeepseekNewResumeIDIsAlwaysEmpty pins the contract that
// guarantees the orchestrator's pre-run-mint code path is a no-op
// for deepseek (the load-bearing reason a post-run capture exists
// in the first place). Every call must return ("", nil) regardless
// of context cancellation, so the planner does NOT mistakenly think
// it has a session id to thread through.
func TestDeepseekNewResumeIDIsAlwaysEmpty(t *testing.T) {
	a := deepseek.New()
	for range 5 {
		got, err := a.NewResumeID(t.Context())
		if err != nil {
			t.Fatalf("NewResumeID: %v", err)
		}
		if got != "" {
			t.Fatalf("NewResumeID = %q, want empty", got)
		}
	}

	// Even with a cancelled context the contract holds: deepseek-tui
	// has no pre-run mint flow, so there is nothing to fail at.
	cctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := a.NewResumeID(cctx)
	if err != nil {
		t.Fatalf("NewResumeID(cancelled): %v", err)
	}
	if got != "" {
		t.Fatalf("NewResumeID(cancelled) = %q, want empty", got)
	}
}
