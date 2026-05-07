package picker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/store/tasks"
)

// PickTask renders the shared task-list select with the supplied
// title. Labels follow "<id> — <status> — <summary>"; an empty
// summary collapses to "(no summary)" so every row stays selectable.
//
// Contract:
//   - empty tasks      → ("", false, nil)
//   - huh.ErrUserAborted (Ctrl-C / Esc) → ("", false, nil)
//   - chosen           → (id, true, nil)
//   - other UI error   → ("", false, wrapped)
//
// tasks is expected to be pre-sorted by the caller (orchestrators run
// tasks.SortTasks first). ok=false collapses the abort and empty
// branches so callers treat them uniformly as "no selection".
func (p *Picker) PickTask(ctx context.Context, title string, tasks []tasks.Task) (string, bool, error) {
	if len(tasks) == 0 {
		return "", false, nil
	}
	labels, byLabel := FormatTaskLabels(tasks)
	chosen, err := p.choose(ctx, title, labels)
	if errors.Is(err, huh.ErrUserAborted) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	id, ok := byLabel[chosen]
	if !ok {
		return "", false, fmt.Errorf("picker: unknown selection %q", chosen)
	}
	return id, true, nil
}

// FormatTaskLabels mirrors the label format the picker uses on the
// terminal (`<id> — <status> — <summary>`, with empty summaries
// collapsed to "(no summary)"). Exposed so test harnesses can build
// the same option list the production picker would render.
func FormatTaskLabels(tasks []tasks.Task) ([]string, map[string]string) {
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
