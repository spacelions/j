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

// BuildPlanner composes the planner's shared instruction with a
// pointer to the user's markdown task. The agent is told to read the
// file from disk rather than receiving its contents inline so the
// prompt stays small and there is no chance of drift between the
// rendered prompt and the on-disk source.
//
// mustread, when non-empty, is rendered as a bulleted "Before
// starting, read these project files…" block between the instruction
// and the user-request line. An empty / nil mustread leaves the
// prompt byte-identical to the pre-mustread output.
func BuildPlanner(targetPath string, mustread []string) string {
	return fmt.Sprintf(
		"%s%s\n\nRead the user request at %q before planning.",
		strings.TrimSpace(planner.Instruction),
		mustreadSuffix(mustread),
		targetPath,
	)
}

// BuildPlannerResume composes the resume-only planner prompt: it
// asks the agent to inspect the previous session, check what was
// already done, summarise it for the user, and continue only what is
// still outstanding. The original requirement markdown path is
// referenced for context only — there is no instruction to re-plan
// from scratch and no instruction to save requirements.md / plan.md,
// so resumed cursor sessions stop overwriting the prior artifacts.
//
// The full planner.Instruction body is embedded so the resumed
// session has the same coding rules available as the first-run
// BuildPlanner did. The instruction text itself opens with
// "You are the planner in a planner/worker/verifier workflow.",
// so this builder relies on that opening as the role preamble
// rather than emitting a duplicate sentence.
func BuildPlannerResume(targetPath string) string {
	return fmt.Sprintf(
		"%s\n\n"+
			"You are resuming a previous planning session. "+
			"Check what was already done in the previous turn, "+
			"summarise the prior progress for the user in one short paragraph, "+
			"and then continue only the work that is still outstanding. "+
			"Do not re-plan from scratch and do not overwrite the saved "+
			"requirements.md / plan.md unless new information forces a change.\n\n"+
			"Original user request lives at %q; read it if you need context.",
		strings.TrimSpace(planner.Instruction),
		targetPath,
	)
}
