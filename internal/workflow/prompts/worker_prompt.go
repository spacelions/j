package prompts

import (
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/workflow/agents/worker"
)

// BuildWorker composes the worker's shared instruction with a
// pointer to the plan markdown the agent must read. The plan body is
// not embedded inline — the agent is expected to open the file from
// disk so the prompt stays small and there is no risk of drift
// between the rendered prompt and the on-disk plan.
//
// A non-empty worktree appends a single trailing line telling the
// worker which git worktree to use for this task; an empty worktree
// leaves the prompt unchanged so the worker behaves as before.
//
// mustread, when non-empty, is rendered as a bulleted "Before
// starting, read these project files…" block between the instruction
// and the read-the-plan line. An empty / nil mustread leaves the
// prompt unchanged.
func BuildWorker(planPath, worktree string, mustread []string) string {
	return appendWorktreeLine(
		fmt.Sprintf(
			"%s%s\n\nRead the plan at %q before starting.",
			strings.TrimSpace(worker.Instruction),
			mustreadSuffix(mustread),
			planPath,
		),
		worktree,
	)
}

// BuildWorkerResume composes the resume-only worker prompt: the
// agent inspects the previous session, checks what was already
// implemented, summarises it for the user, and continues only the
// outstanding work. The plan path is referenced for context only —
// there is no instruction to re-implement from scratch.
//
// The full worker.Instruction body is embedded so the resumed
// session has the same coding rules available as the first-run
// BuildWorker did. The instruction text itself opens with
// "You are the worker in a planner/worker/verifier workflow.",
// so this builder relies on that opening as the role preamble
// rather than emitting a duplicate sentence. A non-empty worktree
// appends the same worktree-direction line as BuildWorker.
func BuildWorkerResume(planPath, worktree string) string {
	return appendWorktreeLine(
		fmt.Sprintf(
			"%s\n\n"+
				"You are resuming a previous coding session. "+
				"Check what was already implemented in the previous turn, "+
				"summarise the prior progress for the user in one short paragraph, "+
				"and then continue only the work that is still outstanding. "+
				"Do not re-implement from scratch.\n\n"+
				"The plan lives at %q; read it for context only.",
			strings.TrimSpace(worker.Instruction),
			planPath,
		),
		worktree,
	)
}

// appendWorktreeLine returns prompt unchanged when worktree is empty
// and otherwise appends a single trailing line telling the worker /
// verifier which git worktree to operate against. Centralising the
// phrasing in one helper keeps BuildWorker / BuildWorkerResume /
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
