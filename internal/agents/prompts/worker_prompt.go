package prompts

import (
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/agents/instructions"
	"github.com/spacelions/j/internal/store"
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
// mustRead, when non-empty, is rendered as a bulleted "Before
// starting, read these project files…" block at the very top of the
// prompt (above the role body). An empty / nil mustRead leaves the
// prompt unchanged.
//
// clarificationPath is the per-task absolute path the agent must
// write a clarification question to — and exit — instead of
// guessing. The contract is rendered by appendClarification (whose
// text lives in instructions/clarification.md), keeping it out of
// the user-overridable worker.md body so a custom worker.md cannot
// silently drop the escape hatch.
func BuildWorker(
	planPath, worktree string, mustRead []string,
	clarificationPath string,
) string {
	return appendClarification(
		appendWorktreeLine(
			prependMustRead(
				fmt.Sprintf(
					"%s\n\n"+strings.TrimSpace(instructions.WorkerPlan),
					strings.TrimSpace(Resolve(store.BucketWorker)),
					planPath,
				),
				mustRead,
			),
			worktree,
		),
		clarificationPath,
	)
}

// BuildWorkerResume composes the resume-only worker prompt: the
// agent inspects the previous session, checks what was already
// implemented, summarises it for the user, and continues only the
// outstanding work. The plan path is referenced for context only —
// there is no instruction to re-implement from scratch.
// clarificationPath carries the same per-task escape hatch
// BuildWorker uses, rendered by appendClarification.
//
// The full instructions.Worker body is embedded so the resumed
// session has the same coding rules available as the first-run
// BuildWorker did. The instruction text itself opens with
// "You are the worker in a planner/worker/verifier workflow.",
// so this builder relies on that opening as the role preamble
// rather than emitting a duplicate sentence. A non-empty worktree
// appends the same worktree-direction line as BuildWorker.
//
// mustRead, when non-empty, is rendered as a bulleted "Before
// starting, read these project files…" block at the very top of
// the prompt (mirroring BuildWorker). An empty / nil mustRead
// leaves the prompt byte-identical to the pre-must-read output.
func BuildWorkerResume(
	planPath, worktree string, mustRead []string,
	clarificationPath string,
) string {
	return appendClarification(
		appendWorktreeLine(
			prependMustRead(
				fmt.Sprintf(
					"%s\n\n"+strings.TrimSpace(instructions.WorkerResume),
					strings.TrimSpace(Resolve(store.BucketWorker)),
					planPath,
				),
				mustRead,
			),
			worktree,
		),
		clarificationPath,
	)
}

// BuildWorkerClarificationResume composes the resume-from-
// clarification worker prompt. It replaces BuildWorker /
// BuildWorkerResume on a resume run that started from
// needs-clarification: the agent reads the per-task
// clarification.md (cited twice — once to read, once to delete),
// restates the question to the user in this session, captures the
// reply, addresses it, and deletes the file before exiting so
// Finish() routes to the natural terminal status. The exit
// contract is appended by appendClarification at the very end of
// the prompt so a custom worker.md cannot drop the rewrite-to-
// reroute branch.
func BuildWorkerClarificationResume(
	planPath, worktree string, mustRead []string,
	clarificationPath string,
) string {
	return appendClarification(
		appendWorktreeLine(
			prependMustRead(
				fmt.Sprintf(
					"%s\n\n"+strings.TrimSpace(
						instructions.WorkerClarificationResume,
					),
					strings.TrimSpace(Resolve(store.BucketWorker)),
					clarificationPath, clarificationPath, planPath,
				),
				mustRead,
			),
			worktree,
		),
		clarificationPath,
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
		"%s\n\n"+strings.TrimSpace(instructions.WorkerWorktree),
		prompt, worktree,
	)
}
