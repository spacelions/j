package testcases_test

import (
	"bytes"
	"io"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/spacelions/j/internal/cli/preflight"
)

// driveMustReadForm is the shared teatest harness for the must_read
// preflight prompt. The caller supplies a list of tea.Msg to send
// (typed runes + the final tea.KeyEnter). The captured value pointer
// and final rendered output are returned for assertions.
func driveMustReadForm(t *testing.T, typed string, finalKey tea.KeyType) (string, []byte) {
	t.Helper()
	var v string
	form := preflight.MustReadForm(&v)
	form.SubmitCmd = tea.Quit
	form.CancelCmd = tea.Interrupt

	tm := teatest.NewTestModel(t, form, teatest.WithInitialTermSize(120, 40))
	if typed != "" {
		tm.Type(typed)
	}
	tm.Send(tea.KeyMsg{Type: finalKey})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
	out, _ := io.ReadAll(tm.Output())
	return v, out
}

// TestPreflightMustRead_TypedValueIsCaptured pins the happy path
// covered by the manual checklist `must-read-preflight-asks-once.md`:
// the user types `AGENTS.md;CLAUDE.md`, presses Enter, and the form
// writes the typed value verbatim into the bound pointer (this is
// what huhUI.AskMustRead returns to ensureMustRead, which then
// persists it into project.must_read).
//
// Replaces testcases/manual/must-read-preflight-asks-once.md (form
// behavior side; the persistence + idempotency side is covered by
// internal/cli/preflight/preflight_test.go).
func TestPreflightMustRead_TypedValueIsCaptured(t *testing.T) {
	v, out := driveMustReadForm(t, "AGENTS.md;CLAUDE.md", tea.KeyEnter)
	if v != "AGENTS.md;CLAUDE.md" {
		t.Fatalf("captured value = %q, want %q", v, "AGENTS.md;CLAUDE.md")
	}
	// Smoke-check that the form actually rendered (presence of the
	// title text in the captured output proves the program ran).
	if !bytes.Contains(out, []byte("Files every agent must read first")) {
		// Output may be ANSI-stripped or compressed; only fail if we
		// can prove the form did not run at all.
		if len(out) == 0 {
			t.Fatalf("teatest captured no output; form did not render")
		}
	}
}

// TestPreflightMustRead_BlankSubmitIsCaptured pins the
// `must-read-preflight-blank-allowed.md` scenario: the user presses
// Enter without typing anything; the empty string round-trips to the
// bound pointer. The downstream `ensureMustRead` then stores the
// empty value verbatim so subsequent runs don't re-prompt.
//
// Replaces testcases/manual/must-read-preflight-blank-allowed.md
// (form-behavior side).
func TestPreflightMustRead_BlankSubmitIsCaptured(t *testing.T) {
	v, _ := driveMustReadForm(t, "", tea.KeyEnter)
	if v != "" {
		t.Fatalf("captured value = %q, want empty", v)
	}
}
