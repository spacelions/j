package picker

import (
	"context"

	"github.com/spacelions/j/internal/cli/taskpick"
	"github.com/spacelions/j/internal/store"
)

// PickTask renders the shared task-list picker with the supplied
// title. The title differentiates flows ("Select a task to re-plan",
// "Select a task to work on", etc.) without duplicating the widget
// itself; every cli that needs a task-list prompt routes through
// here so the label format ("<id> — <status> — <summary>") and the
// (id, ok, error) abort/empty contract stay single-sourced.
//
// tasks is expected to be non-empty and pre-sorted by the caller
// (typically via store.SortTasks). The bool return reports whether
// a row was actually selected: ok=false collapses both a user-abort
// (Ctrl-C / Esc) and a defensive empty-input case so callers treat
// them uniformly as "no selection".
func (p *Picker) PickTask(ctx context.Context, title string, tasks []store.Task) (string, bool, error) {
	return taskpick.Pick(ctx, p.in, p.out, title, tasks)
}
