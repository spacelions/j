// Package prompts composes the embedded planner / coder instruction
// markdown with a user-supplied target so every coding-agent backend
// (Cursor, Codex, Claude, ...) sends the same prompt shape. The
// instruction text lives next to the agent that owns it
// (internal/workflow/agents/{planner,coder}/instruction.md) and is
// re-exported from those packages as a string constant.
package prompts

import (
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/workflow/agents/planner"
)

// BuildPlanner composes the planner's shared instruction with the
// user's markdown task. Reusing planner.Instruction keeps the planning
// rules in a single source of truth across every backend.
func BuildPlanner(targetPath, body string) string {
	return fmt.Sprintf(
		"%s\n\nUser request (from %q):\n%s",
		strings.TrimSpace(planner.Instruction),
		targetPath,
		body,
	)
}
