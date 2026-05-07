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
func BuildVerifier(
	reqPath, planPath, verifierPlanPath, findingsPath, worktree string,
	mustRead []string,
) string {
	_ = verifierPlanPath
	return appendVerifierWorktreeLine(
		fmt.Sprintf(
			"%s%s\n\n"+strings.TrimSpace(instructions.VerifierRequest),
			strings.TrimSpace(Resolve(store.BucketVerifier)),
			mustReadSuffix(mustRead),
			reqPath, planPath,
			findingsPath,
		),
		worktree,
	)
}

// BuildVerifierResume composes the resume-only verifier prompt: it
// asks the agent to inspect the previous verification session, check
// what was already done, summarise the prior progress for the user,
// and then continue only the outstanding verification work. The
// requirement / plan paths are referenced for context only — there
// is no instruction to re-verify from scratch.
//
// The full instructions.Verifier body is embedded so the resumed
// session has the same review rules available as the first-run
// BuildVerifier did. The instruction text itself opens with
// "You are the verifier in a planner / worker / verifier workflow.",
// so this builder relies on that opening as the role preamble
// rather than emitting a duplicate sentence.
//
// mustRead, when non-empty, is rendered as a bulleted "Before
// starting, read these project files…" block between the
// instruction and the resume framing line (mirroring
// BuildPlannerResume). An empty / nil mustRead leaves the prompt
// byte-identical to the pre-must-read output.
func BuildVerifierResume(
	reqPath, planPath, worktree string, mustRead []string,
) string {
	return appendVerifierWorktreeLine(
		fmt.Sprintf(
			"%s%s\n\n"+strings.TrimSpace(instructions.VerifierResume),
			strings.TrimSpace(Resolve(store.BucketVerifier)),
			mustReadSuffix(mustRead),
			reqPath, planPath,
		),
		worktree,
	)
}

// BuildVerifierFix composes the worker-side fix prompt used when
// the outer verify loop has observed a `VERDICT: FAIL` from the
// verifier and wants the previous worker session to address the
// listed findings without re-planning. The plan path is referenced
// for context, the findings path is the action list — both are read
// from disk by the agent rather than being inlined into the prompt.
//
// A fix loop runs the worker (not the verifier), so the full
// instructions.Worker body is embedded. The instruction text itself
// opens with "You are the worker in a planner/worker/verifier
// workflow.", so this builder relies on that opening as the role
// preamble rather than emitting a duplicate sentence.
func BuildVerifierFix(planPath, findingsPath, worktree string) string {
	return appendWorktreeLine(
		fmt.Sprintf(
			"%s\n\n"+strings.TrimSpace(instructions.VerifierFix),
			strings.TrimSpace(Resolve(store.BucketWorker)),
			planPath, findingsPath,
		),
		worktree,
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
