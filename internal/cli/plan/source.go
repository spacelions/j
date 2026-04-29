package plan

import "fmt"

// PlanSource is the planning input the user picks at the start of `j plan`.
// Today the orchestrator dispatches two flavours:
//
//   - SourceMarkdown reads a markdown task description and records the
//     produced plan + refined requirements into <cwd>/.j/tasks/<id>/.
//     This is the implicit source whenever --from-file/PLAN_FROM_FILE
//     is supplied.
//   - SourceLinear is a placeholder for a future Linear-issue source;
//     it currently prints a stub message and returns successfully
//     without invoking any agent.
type PlanSource int

const (
	// SourceMarkdown represents the markdown-file flow; it is the
	// implicit source whenever --from-file is set.
	SourceMarkdown PlanSource = iota
	// SourceLinear is reserved for a future Linear-issue integration.
	SourceLinear
)

// SourceLabels lists the user-facing labels in display order. Keeping
// them adjacent to the parser guarantees the picker and parser can
// never disagree.
var SourceLabels = []string{"markdown file", "linear"}

// String returns the user-facing label for the source.
func (s PlanSource) String() string {
	switch s {
	case SourceMarkdown:
		return "markdown file"
	case SourceLinear:
		return "linear"
	default:
		return fmt.Sprintf("PlanSource(%d)", int(s))
	}
}

// ParseSource maps a user-facing label back to its typed enum.
func ParseSource(label string) (PlanSource, error) {
	switch label {
	case "markdown file":
		return SourceMarkdown, nil
	case "linear":
		return SourceLinear, nil
	}
	return 0, fmt.Errorf("plan: unknown source %q", label)
}
