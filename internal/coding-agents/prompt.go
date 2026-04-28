package codingagents

import (
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/workflow/agents/planner"
)

// BuildPrompt composes the planner's shared instruction with the user's
// markdown task. Reusing planner.Instruction keeps the planning rules in
// a single source of truth (internal/workflow/agents/planner/instruction.md)
// across every Agent backend.
func BuildPrompt(targetPath, body string) string {
	return fmt.Sprintf(
		"%s\n\nUser request (from %q):\n%s",
		strings.TrimSpace(planner.Instruction),
		targetPath,
		body,
	)
}
