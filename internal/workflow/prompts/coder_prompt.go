package prompts

import (
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/workflow/agents/coder"
)

// BuildCoder composes the coder's shared instruction with a plan
// markdown body. Reusing coder.Instruction keeps the coding rules in a
// single source of truth across every backend, mirroring how
// BuildPlanner reuses planner.Instruction for the planning prompt.
func BuildCoder(planPath, body string) string {
	return fmt.Sprintf(
		"%s\n\nPlan (from %q):\n%s",
		strings.TrimSpace(coder.Instruction),
		planPath,
		body,
	)
}
