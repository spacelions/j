package plan

import "fmt"

// PlanSource is the planning input the user picks at the start of `j plan`.
// The orchestrator dispatches three flavours:
//
//   - SourceMarkdown reads a markdown task description and records the
//     produced plan + refined requirements into <cwd>/.j/tasks/<id>/.
//     This is the implicit source whenever --from-file/PLAN_FROM_FILE
//     is supplied.
//   - SourceLinear is a placeholder for a future Linear-issue source;
//     it currently prints a stub message and returns successfully
//     without invoking any agent.
//   - SourceTask resumes the markdown task description of an existing
//     `<cwd>/.j/tasks/<id>/` and re-runs the planner against the
//     same task row (a "re-plan"). The flow is reachable both via
//     `--from-task <id>` and via the no-flag picker.
type PlanSource int

const (
	// SourceMarkdown represents the markdown-file flow; it is the
	// implicit source whenever --from-file is set.
	SourceMarkdown PlanSource = iota
	// SourceLinear is reserved for a future Linear-issue integration.
	SourceLinear
	// SourceTask represents the re-plan flow: an existing task is
	// chosen (via --from-task or a picker) and the planner is
	// re-run against its existing requirements.md, mutating the
	// task row in place.
	SourceTask
)

// SourceLabels lists the user-facing labels in display order. Keeping
// them adjacent to the parser guarantees the picker and parser can
// never disagree. The "re-plan an existing task" entry is the
// long-form label so the picker reads naturally.
var SourceLabels = []string{"markdown", "linear", "re-plan an existing task"}

// String returns the user-facing label for the source.
func (s PlanSource) String() string {
	switch s {
	case SourceMarkdown:
		return "markdown"
	case SourceLinear:
		return "linear"
	case SourceTask:
		return "re-plan an existing task"
	default:
		return fmt.Sprintf("PlanSource(%d)", int(s))
	}
}

// ParseSource maps a user-facing label back to its typed enum. The
// previous "markdown file" spelling is intentionally rejected: a
// stale config surfaces a clear "unknown source" error instead of
// silently working.
func ParseSource(label string) (PlanSource, error) {
	switch label {
	case "markdown":
		return SourceMarkdown, nil
	case "linear":
		return SourceLinear, nil
	case "re-plan an existing task":
		return SourceTask, nil
	}
	return 0, fmt.Errorf("plan: unknown source %q", label)
}
