package testcases_test

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/spacelions/j/internal/cli/picker"
	storetasks "github.com/spacelions/j/internal/store/tasks"
)

// TestPickerTask_ArrowDownEnterPicksSecondRow drives the production
// task-picker form via teatest: build the same form picker.PickTask
// renders, send a down-arrow followed by Enter, and assert the
// captured label decodes back to the second task's id via the same
// FormatTaskLabels mapping production uses.
//
// Replaces the picker side of testcases/manual/tasks-picker-interactive.md
// for `j tasks discard` and `j tasks enter` (the picker, the
// subshell-spawn / row-delete sides are covered by existing unit
// tests in internal/cli/tasks/{discard,enter}_test.go).
func TestPickerTask_ArrowDownEnterPicksSecondRow(t *testing.T) {
	rows := []storetasks.Task{
		{ID: "alpha-id", Status: "plan-done", Summary: "first task"},
		{ID: "beta-id", Status: "plan-done", Summary: "second task"},
		{ID: "gamma-id", Status: "plan-done", Summary: "third task"},
	}
	labels, byLabel := picker.FormatTaskLabels(rows)

	var chosen string
	form := picker.SelectForm("Pick a task", labels, &chosen)
	form.SubmitCmd = tea.Quit
	form.CancelCmd = tea.Interrupt

	tm := teatest.NewTestModel(t, form, teatest.WithInitialTermSize(120, 40))
	// down arrow: highlight row 2 ("beta-id"); Enter: submit.
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))

	id, ok := byLabel[chosen]
	if !ok {
		t.Fatalf("chosen label %q not in byLabel map", chosen)
	}
	if id != "beta-id" {
		t.Fatalf("picked id = %q, want beta-id (down arrow then Enter)", id)
	}
}

// TestPickerTask_EnterOnFirstRow pins that submitting without
// navigating selects the first task — the contract every existing
// caller of PickTask relies on.
func TestPickerTask_EnterOnFirstRow(t *testing.T) {
	rows := []storetasks.Task{
		{ID: "alpha-id", Status: "plan-done", Summary: "first"},
		{ID: "beta-id", Status: "plan-done", Summary: "second"},
	}
	labels, byLabel := picker.FormatTaskLabels(rows)

	var chosen string
	form := picker.SelectForm("Pick a task", labels, &chosen)
	form.SubmitCmd = tea.Quit
	form.CancelCmd = tea.Interrupt

	tm := teatest.NewTestModel(t, form, teatest.WithInitialTermSize(120, 40))
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))

	id := byLabel[chosen]
	if id != "alpha-id" {
		t.Fatalf("picked id = %q, want alpha-id (Enter on first row)", id)
	}
}
