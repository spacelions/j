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

// BuildCoderResume composes the resume-only coder prompt: the agent
// inspects the previous session, checks what was already implemented,
// summarises it for the user, and continues only the outstanding
// work. The plan path and body are embedded for context — there is
// no instruction to re-implement from scratch.
//
// As with BuildPlannerResume, this builder does NOT include
// coder.Instruction; the first-run BuildCoder already seeded the
// session with the full coding rules.
func BuildCoderResume(planPath, body string) string {
	return fmt.Sprintf(
		"You are resuming a previous coding session. "+
			"Check what was already implemented in the previous turn, "+
			"summarise the prior progress for the user in one short paragraph, "+
			"and then continue only the work that is still outstanding. "+
			"Do not re-implement from scratch.\n\n"+
			"Plan (from %q), provided for context only:\n%s",
		planPath,
		body,
	)
}
