package plan

import "fmt"

// PlanSource is the planning input the user picks at the start of `j plan`.
// Today the orchestrator dispatches three flavours:
//
//   - SourceMarkdown reads a markdown task description and writes
//     <stem>.plan.md beside it; this is the original behaviour and the
//     only path that runs when --target/PLAN_TARGET is supplied.
//   - SourceScratch starts the agent's plan-mode TUI with no body and no
//     output file expectation, so the user can iterate freely.
//   - SourceLinear is a placeholder for a future Linear-issue source; it
//     currently returns successfully without invoking any agent.
type PlanSource int

const (
	// SourceMarkdown represents the markdown-file flow; it is the
	// implicit source whenever --target is set.
	SourceMarkdown PlanSource = iota
	// SourceScratch starts a free-form plan session with no markdown
	// body and no plan.md output.
	SourceScratch
	// SourceLinear is reserved for a future Linear-issue integration.
	SourceLinear
)

// SourceLabels lists the user-facing labels in display order. Keeping
// them adjacent to the parser guarantees the picker and parser can
// never disagree.
var SourceLabels = []string{"from scratch", "markdown file", "linear"}

// String returns the user-facing label for the source.
func (s PlanSource) String() string {
	switch s {
	case SourceMarkdown:
		return "markdown file"
	case SourceScratch:
		return "from scratch"
	case SourceLinear:
		return "linear"
	default:
		return fmt.Sprintf("PlanSource(%d)", int(s))
	}
}

// ParseSource maps a user-facing label back to its typed enum.
func ParseSource(label string) (PlanSource, error) {
	switch label {
	case "from scratch":
		return SourceScratch, nil
	case "markdown file":
		return SourceMarkdown, nil
	case "linear":
		return SourceLinear, nil
	}
	return 0, fmt.Errorf("plan: unknown source %q", label)
}
