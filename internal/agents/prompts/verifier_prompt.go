package prompts

import (
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/agents/instructions"
	"github.com/spacelions/j/internal/store"
)

// BuildVerifier composes the verifier's shared instruction with
// pointers to the requirement and plan markdown the agent must read,
// plus the findings output path the agent must write before exiting.
// The bodies are not embedded inline — the agent opens the files
// itself so the prompt stays small and there is no risk of drift
// between the rendered prompt and the on-disk markdown.
//
// Reusing instructions.Verifier keeps the review rules in a single
// source of truth across every backend, mirroring how BuildPlanner
// reuses planner.Instruction and BuildWorker reuses
// instructions.Worker.
//
// verifierPlanPath stays in the signature even though the body does
// not reference it: the value is the agent's draft plan output path
// and the builder simply does not surface it in the prompt because
// the agent learns the path through the same instruction body that
// drives the save behaviour.
//
// clarificationPath is the per-task absolute path the agent must
// write to instead of guessing if it gets stuck; the contract is
// rendered by appendClarification at the very end of the prompt.
// The `VERDICT: PASS|FAIL` last-line contract that depends on
// findingsPath also lives in verifier_request.md so a custom
// verifier.md cannot drop it.
func BuildVerifier(
	reqPath, planPath, verifierPlanPath, findingsPath, worktree string,
	mustRead []string, clarificationPath string,
) string {
	_ = verifierPlanPath
	return appendClarification(
		appendVerifierWorktreeLine(
			prependMustRead(
				fmt.Sprintf(
					"%s\n\n"+strings.TrimSpace(instructions.VerifierRequest),
					strings.TrimSpace(Resolve(store.BucketVerifier)),
					reqPath, planPath, findingsPath,
				),
				mustRead,
			),
			worktree,
		),
		clarificationPath,
	)
}

// BuildVerifierResume composes the resume-only verifier prompt: it
// asks the agent to inspect the previous verification session, check
// what was already done, summarise the prior progress for the user,
// and then continue only the outstanding verification work. The
// requirement / plan paths are referenced for context only — there
// is no instruction to re-verify from scratch. clarificationPath
// carries the per-task escape hatch onto the resume turn (rendered
// by appendClarification).
//
// The full instructions.Verifier body is embedded so the resumed
// session has the same review rules available as the first-run
// BuildVerifier did. The instruction text itself opens with
// "You are the verifier in a planner / worker / verifier workflow.",
// so this builder relies on that opening as the role preamble
// rather than emitting a duplicate sentence.
//
// mustRead, when non-empty, is rendered as a bulleted "Before
// starting, read these project files…" block at the very top of the
// prompt (mirroring BuildVerifier). An empty / nil mustRead leaves
// the prompt byte-identical to the pre-must-read output.
func BuildVerifierResume(
	reqPath, planPath, worktree string, mustRead []string,
	clarificationPath string,
) string {
	return appendClarification(
		appendVerifierWorktreeLine(
			prependMustRead(
				fmt.Sprintf(
					"%s\n\n"+strings.TrimSpace(instructions.VerifierResume),
					strings.TrimSpace(Resolve(store.BucketVerifier)),
					reqPath, planPath,
				),
				mustRead,
			),
			worktree,
		),
		clarificationPath,
	)
}

// BuildVerifierFix composes the worker-side fix prompt used when
// the outer verify loop has observed a `VERDICT: FAIL` from the
// verifier and wants the previous worker session to address the
// listed findings without re-planning. The plan path is referenced
// for context, the findings path is the action list — both are read
// from disk by the agent rather than being inlined into the prompt.
// clarificationPath carries the same per-task escape hatch that
// BuildWorker uses (rendered by appendClarification) so a custom
// worker.md cannot silently drop it.
//
// A fix loop runs the worker (not the verifier), so the full
// instructions.Worker body is embedded. The instruction text itself
// opens with "You are the worker in a planner/worker/verifier
// workflow.", so this builder relies on that opening as the role
// preamble rather than emitting a duplicate sentence.
func BuildVerifierFix(
	planPath, findingsPath, worktree, clarificationPath string,
) string {
	return appendClarification(
		appendWorktreeLine(
			fmt.Sprintf(
				"%s\n\n"+strings.TrimSpace(instructions.VerifierFix),
				strings.TrimSpace(Resolve(store.BucketWorker)),
				planPath, findingsPath,
			),
			worktree,
		),
		clarificationPath,
	)
}

// appendVerifierWorktreeLine returns prompt unchanged when worktree
// is empty and otherwise appends a single trailing line telling the
// verifier which git worktree to inspect. The phrasing is intentionally
// different from appendWorktreeLine in worker_prompt.go: the verifier
// does not create worktrees, it only resolves them via
// `git worktree list` from the repository root.
func appendVerifierWorktreeLine(prompt, worktree string) string {
	if worktree == "" {
		return prompt
	}
	return fmt.Sprintf(
		"%s\n\n"+strings.TrimSpace(instructions.VerifierWorktree),
		prompt, worktree,
	)
}
