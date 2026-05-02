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
//
// A non-empty worktree appends a single trailing line telling the
// coder which git worktree to use for this task; an empty worktree
// leaves the prompt unchanged so the coder behaves as before.
//
// mustread, when non-empty, is rendered as a bulleted "Before
// starting, read these project files…" block between the instruction
// and the plan. An empty / nil mustread leaves the prompt unchanged.
func BuildCoder(planPath, body, worktree string, mustread []string) string {
	return appendWorktreeLine(
		fmt.Sprintf(
			"%s%s\n\nPlan (from %q):\n%s",
			strings.TrimSpace(coder.Instruction),
			mustreadSuffix(mustread),
			planPath,
			body,
		),
		worktree,
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
// session with the full coding rules. A non-empty worktree appends
// the same worktree-direction line as BuildCoder.
func BuildCoderResume(planPath, body, worktree string) string {
	return appendWorktreeLine(
		fmt.Sprintf(
			"You are resuming a previous coding session. "+
				"Check what was already implemented in the previous turn, "+
				"summarise the prior progress for the user in one short paragraph, "+
				"and then continue only the work that is still outstanding. "+
				"Do not re-implement from scratch.\n\n"+
				"Plan (from %q), provided for context only:\n%s",
			planPath,
			body,
		),
		worktree,
	)
}

// appendWorktreeLine returns prompt unchanged when worktree is empty
// and otherwise appends a single trailing line telling the coder /
// verifier which git worktree to operate against. Centralising the
// phrasing in one helper keeps BuildCoder / BuildCoderResume /
// BuildVerifierFix byte-identical on the suffix so prompt tests can
// assert the same substring uniformly.
func appendWorktreeLine(prompt, worktree string) string {
	if worktree == "" {
		return prompt
	}
	return fmt.Sprintf(
		"%s\n\nUse the git worktree named %q for this task; "+
			"create it via `git worktree add` if it does not yet exist.",
		prompt, worktree,
	)
}
