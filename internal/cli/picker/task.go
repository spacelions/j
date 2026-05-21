package picker

import (
	"context"
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/store/tasks"
)

// PickTask renders the shared task-list select with the supplied
// title. Labels follow "<id> — <status> — <summary>"; an empty
// summary collapses to "(no summary)" so every row stays selectable.
//
// Contract:
//   - empty tasks      → ("", false, nil)
//   - chosen           → (id, true, nil)
//   - UI error         → ("", false, wrapped) — callers with a
//     resolver.CleanAbort defer convert huh.ErrUserAborted to nil
//
// tasks is expected to be pre-sorted by the caller (orchestrators run
// tasks.SortTasks first).
func (p *Picker) PickTask(
	ctx context.Context, title string, tasks []tasks.Task,
) (string, bool, error) {
	if len(tasks) == 0 {
		return "", false, nil
	}
	labels, byLabel := FormatTaskLabels(tasks)
	chosen, err := p.choose(ctx, title, labels)
	if err != nil {
		return "", false, err
	}
	return byLabel[chosen], true, nil
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
