package claude

import (
	"github.com/spacelions/j/internal/agents/prompts"
	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// buildPlanPrompt picks the right planner prompt for req. Mirrors the
// cursor backend: a fresh run composes the planner instruction; a
// resume run switches to the resume-only template that asks the
// previous session to inspect / report / continue. Both branches
// receive the same save-and-exit suffix via
// prompts.AppendPlannerSaveSuffix (which threads the per-task
// clarification path) so the reaper sees identical artifacts in
// either case.
func buildPlanPrompt(req codingagents.PlanRequest) string {
	base := prompts.BuildPlanner(req.FromFilePath, req.MustRead)
	if req.ResumeFromClarification {
		base = prompts.BuildPlannerClarificationResume(
			req.FromFilePath, req.ClarificationPath, req.MustRead,
		)
	} else if req.Resume {
		base = prompts.BuildPlannerResume(req.FromFilePath, req.MustRead)
	}
	return prompts.AppendPlannerSaveSuffix(
		base, req.RequirementsOutputPath, req.PlanOutputPath,
		req.ClarificationPath,
	)
}

// buildWorkPrompt picks the right worker prompt for req. The
// fix-findings branch wins first (FixFindings=true means the outer
// verify loop wants the previous worker session to address a
// concrete set of verifier findings — read by the agent from the
// per-task verifier_findings.md absolute path threaded in via
// req.VerifierFindingsOutputPath — without re-planning). Resume runs
// are next; first-run falls through. Every branch threads
// req.ClarificationPath so the per-task escape hatch is preserved.
func buildWorkPrompt(req codingagents.WorkRequest) string {
	if req.FixFindings {
		return prompts.BuildVerifierFix(
			req.PlanPath, req.VerifierFindingsOutputPath,
			req.Worktree, req.ClarificationPath,
		)
	}
	if req.ResumeFromClarification {
		return prompts.BuildWorkerClarificationResume(
			req.PlanPath, req.Worktree, req.MustRead,
			req.ClarificationPath,
		)
	}
	if req.Resume {
		return prompts.BuildWorkerResume(
			req.PlanPath, req.Worktree, req.MustRead,
			req.ClarificationPath,
		)
	}
	return prompts.BuildWorker(
		req.PlanPath, req.Worktree, req.MustRead, req.ClarificationPath,
	)
}

// buildVerifyPrompt picks the right verifier prompt for req. Resume
// runs switch to the resume-only template; first-run uses the full
// verifier instruction with the save-plan / save-findings suffix.
// Both branches thread req.ClarificationPath so the per-task escape
// hatch is preserved.
func buildVerifyPrompt(req codingagents.VerifyRequest) string {
	if req.ResumeFromClarification {
		return prompts.BuildVerifierClarificationResume(
			req.RequirementsPath, req.PlanPath, req.Worktree,
			req.MustRead, req.ClarificationPath,
		)
	}
	if req.Resume {
		return prompts.BuildVerifierResume(
			req.RequirementsPath, req.PlanPath, req.Worktree,
			req.MustRead, req.ClarificationPath,
		)
	}
	return prompts.BuildVerifier(
		req.RequirementsPath,
		req.PlanPath,
		req.VerifierPlanOutputPath, req.VerifierFindingsOutputPath,
		req.Worktree,
		req.MustRead, req.ClarificationPath,
	)
}
