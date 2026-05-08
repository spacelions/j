package cursor

import (
	"github.com/spacelions/j/internal/agents/prompts"
	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// buildPlanPrompt picks the right planner prompt for req. On a fresh
// run it composes the full planner instruction; on a resume run it
// switches to the resume-only template that asks the previous cursor
// session to inspect / report / continue. Both branches then receive
// the same save-and-exit suffix via prompts.AppendPlannerSaveSuffix
// (which also threads the per-task clarification path) so the reaper
// sees identical artifacts in either case (a help-status row whose
// first run skipped the artifacts must still produce them on
// resume).
func buildPlanPrompt(req codingagents.PlanRequest) string {
	base := prompts.BuildPlanner(req.FromFilePath, req.MustRead)
	if req.Resume {
		base = prompts.BuildPlannerResume(req.FromFilePath, req.MustRead)
	}
	return prompts.AppendPlannerSaveSuffix(
		base, req.RequirementsOutputPath, req.PlanOutputPath,
		req.ClarificationPath,
	)
}

// buildWorkPrompt picks the right worker prompt for req. The
// fix-findings branch wins first: FixFindings=true means the outer
// verify loop wants the previous worker session to address a
// concrete set of verifier findings (read by the agent from the
// per-task verifier_findings.md absolute path threaded in via
// req.VerifierFindingsOutputPath) without re-planning. Resume runs
// are next; first-run falls through to the full worker instruction.
// Every branch threads req.Worktree and req.ClarificationPath
// through so the prompt carries the worktree-direction line when
// the task has one and always names the per-task escape hatch.
func buildWorkPrompt(req codingagents.WorkRequest) string {
	if req.FixFindings {
		return prompts.BuildVerifierFix(
			req.PlanPath, req.VerifierFindingsOutputPath,
			req.Worktree, req.ClarificationPath,
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
// Both branches thread req.Worktree and req.ClarificationPath
// through so the prompt carries a worktree-direction line when the
// task has one and always names the per-task escape hatch.
func buildVerifyPrompt(req codingagents.VerifyRequest) string {
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
