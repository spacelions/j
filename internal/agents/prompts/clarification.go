package prompts

import (
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/agents/instructions"
)

// appendClarification appends the canonical "if you cannot proceed,
// write your question to <path> and exit" escape hatch to prompt.
// Centralising the contract here (rather than duplicating it in
// every role-specific instruction tail) means a single source of
// truth across planner / worker / verifier prompts — a future
// rewording lands in exactly one file (instructions/clarification.md)
// and propagates to every composed prompt automatically.
func appendClarification(prompt, clarificationPath string) string {
	return fmt.Sprintf(
		"%s\n\n"+strings.TrimSpace(instructions.Clarification),
		prompt, clarificationPath,
	)
}
