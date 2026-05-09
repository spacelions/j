package prompts

import codingagents "github.com/spacelions/j/internal/coding-agents"

// PlanPrompt picks the right planner template for req and appends the
// shared save-and-exit suffix. Precedence is
// ResumeFromClarification > Resume > fresh; both resume branches
// receive the same suffix so the reaper sees identical artefacts on
// resume runs (a help-status row whose first run skipped the
// artefacts must still produce them on resume).
func PlanPrompt(req codingagents.PlanRequest) string {
	base := BuildPlanner(req.FromFilePath, req.MustRead)
	switch {
	case req.ResumeFromClarification:
		base = BuildPlannerClarificationResume(
			req.FromFilePath, req.ClarificationPath, req.MustRead,
		)
	case req.Resume:
		base = BuildPlannerResume(req.FromFilePath, req.MustRead)
	}
	return AppendPlannerSaveSuffix(
		base, req.RequirementsOutputPath, req.PlanOutputPath,
		req.ClarificationPath,
	)
}

// WorkPrompt picks the right worker template for req. Precedence:
// FixFindings > ResumeFromClarification > Resume > fresh.
func WorkPrompt(req codingagents.WorkRequest) string {
	switch {
	case req.FixFindings:
		return BuildVerifierFix(
			req.PlanPath, req.VerifierFindingsOutputPath,
			req.Worktree, req.ClarificationPath,
		)
	case req.ResumeFromClarification:
		return BuildWorkerClarificationResume(
			req.PlanPath, req.Worktree, req.MustRead,
			req.ClarificationPath,
		)
	case req.Resume:
		return BuildWorkerResume(
			req.PlanPath, req.Worktree, req.MustRead,
			req.ClarificationPath,
		)
	}
	return BuildWorker(
		req.PlanPath, req.Worktree, req.MustRead, req.ClarificationPath,
	)
}

// VerifyPrompt picks the right verifier template for req. Precedence:
// ResumeFromClarification > Resume > fresh.
func VerifyPrompt(req codingagents.VerifyRequest) string {
	switch {
	case req.ResumeFromClarification:
		return BuildVerifierClarificationResume(
			req.RequirementsPath, req.PlanPath, req.Worktree,
			req.MustRead, req.ClarificationPath,
		)
	case req.Resume:
		return BuildVerifierResume(
			req.RequirementsPath, req.PlanPath, req.Worktree,
			req.MustRead, req.ClarificationPath,
		)
	}
	return BuildVerifier(
		req.RequirementsPath, req.PlanPath,
		req.VerifierPlanOutputPath, req.VerifierFindingsOutputPath,
		req.Worktree, req.MustRead, req.ClarificationPath,
	)
}
