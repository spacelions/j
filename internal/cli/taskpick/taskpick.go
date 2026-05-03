// Package taskpick renders the shared "pick a task" select widget
// used by the j subcommands (plan, work, verify, tasks). The widget
// is a single huh.NewSelect[string] over store.Task rows; the label
// shape is "<id> — <status> — <summary>" and the contract is the
// same across every caller:
//
//   - empty tasks       → ("", false, nil)
//     Defensive: callers should pre-screen, but if a stray empty
//     slice reaches Pick we collapse to "ok=false" so the caller
//     can short-circuit cleanly without spelling out a bespoke
//     error.
//   - huh.ErrUserAborted → ("", false, nil)
//     User pressed Ctrl-C / Esc. Callers treat ok=false as a
//     clean cancel, mirroring the package's existing
//     huh-abort-collapses-to-nil convention.
//   - chosen             → (id,  true,  nil)
//   - other error        → ("",  false, fmt.Errorf("taskpick: %w", err))
//
// Each call drives a fresh huh.Form so callers can call Pick
// repeatedly within the same orchestrator without leaking state
// between invocations.
package taskpick

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store"
)

// Pick renders a select widget over tasks under the supplied
// title and returns (id, true, nil) for the chosen row. See the
// package doc for the full contract on empty input, abort, and
// errors. tasks is expected to be pre-sorted by the caller (the
// orchestrators run store.SortTasks before invoking Pick).
func Pick(ctx context.Context, in io.Reader, out io.Writer, title string, tasks []store.Task) (string, bool, error) {
	if len(tasks) == 0 {
		return "", false, nil
	}
	labels, byLabel := FormatLabels(tasks)
	var chosen string
	err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(title).
			Options(huh.NewOptions(labels...)...).
			Filtering(true).
			Value(&chosen),
	)).WithInput(in).WithOutput(out).WithTheme(uitheme.Theme()).RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("taskpick: %w", err)
	}
	id, hit := byLabel[chosen]
	if !hit {
		return "", false, fmt.Errorf("taskpick: unknown selection %q", chosen)
	}
	return id, true, nil
}

// FormatLabels builds the "<id> — <status> — <summary>" label list
// and a reverse map so callers can resolve the chosen label back
// to a task id. Empty Summary collapses to "(no summary)" so every
// row stays selectable. Exposed as package API so unit tests can
// pin the label shape without driving a real huh form.
func FormatLabels(tasks []store.Task) ([]string, map[string]string) {
	labels := make([]string, 0, len(tasks))
	byLabel := make(map[string]string, len(tasks))
	for _, t := range tasks {
		summary := strings.TrimSpace(t.Summary)
		if summary == "" {
			summary = "(no summary)"
		}
		label := fmt.Sprintf("%s — %s — %s", t.ID, t.Status, summary)
		labels = append(labels, label)
		byLabel[label] = t.ID
	}
	return labels, byLabel
}
