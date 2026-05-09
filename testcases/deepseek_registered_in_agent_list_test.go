package testcases_test

import (
	"context"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/coding-agents/deepseek"
)

// TestDeepseekIsThirdRegisteredAgent pins the picker / settings
// acceptance criterion: "The tool picker for planner, worker, and
// verifier offers a third entry, deepseek, in addition to the
// existing cursor and claude options."
//
// The j cli builds the agents slice the same way at every
// registration site (start.go, orchestrate.go, resume_*.go,
// re_*.go, continue.go). This test mirrors that construction and
// asserts the picker would see all three names — and only those
// three — so a regression that drops one or accidentally registers
// a fourth backend fails the build.
func TestDeepseekIsThirdRegisteredAgent(t *testing.T) {
	agents := []codingagents.Agent{
		cursor.New(), claude.New(), deepseek.New(),
	}

	want := []string{"cursor", "claude", "deepseek"}
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

// TestDeepseekIsTheOnlyResumeIDCapturer pins the design boundary
// the plan calls out: cursor and claude mint their session id
// pre-run and intentionally do NOT implement ResumeIDCapturer; only
// deepseek does, so the orchestrator's post-run capture step is a
// no-op for cursor / claude users (acceptance criteria: "Existing
// users who already pinned cursor or claude see no change in
// behaviour.").
func TestDeepseekIsTheOnlyResumeIDCapturer(t *testing.T) {
	cases := []struct {
		name           string
		agent          codingagents.Agent
		wantImplements bool
	}{
		{"cursor", cursor.New(), false},
		{"claude", claude.New(), false},
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
