package prompts

import (
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
)

// PlanPrompt picks the right planner template for req and appends the
// shared save-and-exit suffix. Precedence is
// ResumeFromClarification > Resume > fresh; both resume branches
// receive the same suffix so the reaper sees identical artefacts on
// resume runs (a help-status row whose first run skipped the
// artefacts must still produce them on resume).
func PlanPrompt(req codingagents.PlanRequest) string {
	if req.PRFeedback != nil {
		return BuildPRFeedbackPlannerPrompt(
			*req.PRFeedback, req.PlanOutputPath, req.MustRead,
		)
	}
	paths := tasks.TaskPaths{
		Requirements:  req.RequirementsOutputPath,
		Plan:          req.PlanOutputPath,
		Clarification: req.ClarificationPath,
	}
	base := BuildPlannerPrompt(req.FromFilePath, req.MustRead)
	switch {
	case req.ResumeFromClarification:
		base = BuildPlannerClarificationResumePrompt(
			req.FromFilePath, req.ClarificationPath, req.MustRead,
		)
	case req.Resume:
		base = BuildPlannerResumePrompt(req.FromFilePath, req.MustRead)
	}
	return AppendPlannerSaveSuffix(base, paths)
}

// WorkPrompt picks the right worker template for req. Precedence:
// FixFindings > ResumeFromClarification > Resume > fresh.
func WorkPrompt(req codingagents.WorkRequest) string {
	paths := tasks.TaskPaths{
		Plan:          req.PlanPath,
		Findings:      req.VerifierFindingsOutputPath,
		Clarification: req.ClarificationPath,
	}
	switch {
	case req.FixFindings:
		return BuildVerifierFixPrompt(paths, req.Worktree)
	case req.ResumeFromClarification:
		return BuildWorkerClarificationResumePrompt(
			paths, req.Worktree, req.MustRead)
	case req.Resume:
		return BuildWorkerResumePrompt(paths, req.Worktree, req.MustRead)
	}
	return BuildWorkerPrompt(paths, req.Worktree, req.MustRead)
}

// VerifyPrompt picks the right verifier template for req. Precedence:
// ResumeFromClarification > Resume > fresh.
func VerifyPrompt(req codingagents.VerifyRequest) string {
	paths := tasks.TaskPaths{
		Requirements:  req.RequirementsPath,
		Plan:          req.PlanPath,
		VerifierPlan:  req.VerifierPlanOutputPath,
		Findings:      req.VerifierFindingsOutputPath,
		Clarification: req.ClarificationPath,
	}
	switch {
	case req.ResumeFromClarification:
		return BuildVerifierClarificationResumePrompt(
			paths, req.Worktree, req.MustRead)
	case req.Resume:
		return BuildVerifierResumePrompt(paths, req.Worktree, req.MustRead)
	}
	return BuildVerifierPrompt(paths, req.Worktree, req.MustRead)
}
