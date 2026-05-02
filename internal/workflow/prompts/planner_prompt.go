// Package prompts composes the embedded planner / worker instruction
// markdown with a user-supplied target so every coding-agent backend
// (Cursor, Codex, Claude, ...) sends the same prompt shape. The
// instruction text lives next to the agent that owns it
// (internal/workflow/agents/{planner,worker}/instruction.md) and is
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
//
// mustread, when non-empty, is rendered as a bulleted "Before
// starting, read these project files…" block between the instruction
// and the user request. An empty / nil mustread leaves the prompt
// byte-identical to the pre-mustread output.
func BuildPlanner(targetPath, body string, mustread []string) string {
	return fmt.Sprintf(
		"%s%s\n\nUser request (from %q):\n%s",
		strings.TrimSpace(planner.Instruction),
		mustreadSuffix(mustread),
		targetPath,
		body,
	)
}

// BuildPlannerResume composes the resume-only planner prompt: it asks
// the agent to inspect the previous session, check what was already
// done, summarise it for the user, and continue only what is still
// outstanding. The original requirement markdown path and body are
// embedded for context only — there is no instruction to re-plan from
// scratch and no instruction to save requirements.md / plan.md, so
// resumed cursor sessions stop overwriting the prior artifacts.
//
// Crucially this builder does NOT include planner.Instruction; that
// belongs to the first-run BuildPlanner which seeds the session, not
// to the resume turn that picks up where the agent already left off.
func BuildPlannerResume(targetPath, body string) string {
	return fmt.Sprintf(
		"You are resuming a previous planning session. "+
			"Check what was already done in the previous turn, "+
			"summarise the prior progress for the user in one short paragraph, "+
			"and then continue only the work that is still outstanding. "+
			"Do not re-plan from scratch and do not overwrite the saved "+
			"requirements.md / plan.md unless new information forces a change.\n\n"+
			"Original user request (from %q), provided for context only:\n%s",
		targetPath,
		body,
	)
}
