package testcases_test

import (
	"github.com/spacelions/j/internal/agents/prompts"
	"github.com/spacelions/j/internal/store/tasks"
)

// worktreeRef adapts the single-string worktree argument the helper
// signatures kept to the prompts.WorktreeRef builders now take:
// empty stays the zero ref; non-empty pairs the branch with a
// deterministic stand-in for the `<task-dir>/worktree` path.
func worktreeRef(branch string) prompts.WorktreeRef {
	if branch == "" {
		return prompts.WorktreeRef{}
	}
	return prompts.WorktreeRef{
		Branch: branch,
		Path:   "/tmp/.j/tasks/abc/worktree",
	}
}

func plannerSavePrompt(base, req, plan, clarify string) string {
	return prompts.AppendPlannerSaveSuffix(base, tasks.TaskPaths{
		Requirements:  req,
		Plan:          plan,
		Clarification: clarify,
	})
}

func verifierFixPrompt(plan, findings, worktree, clarify string) string {
	return prompts.BuildVerifierFixPrompt(tasks.TaskPaths{
		Plan:          plan,
		Findings:      findings,
		Clarification: clarify,
	}, worktreeRef(worktree))
}

func buildPlannerPrompt(target string, mustRead []string) string {
	return prompts.BuildPlannerPrompt(target, mustRead)
}

func buildPlannerResumePrompt(target string, mustRead []string) string {
	return prompts.BuildPlannerResumePrompt(target, mustRead)
}

func buildWorkerPrompt(
	plan, worktree string,
	mustRead []string,
	clarify string,
) string {
	return prompts.BuildWorkerPrompt(tasks.TaskPaths{
		Plan:          plan,
		Clarification: clarify,
	}, worktreeRef(worktree), mustRead)
}

func buildWorkerResumePrompt(
	plan, worktree string,
	mustRead []string,
	clarify string,
) string {
	return prompts.BuildWorkerResumePrompt(tasks.TaskPaths{
		Plan:          plan,
		Clarification: clarify,
	}, worktreeRef(worktree), mustRead)
}

func buildVerifierPrompt(
	req, plan, _ string,
	findings, worktree string,
	mustRead []string,
	clarify string,
) string {
	return prompts.BuildVerifierPrompt(tasks.TaskPaths{
		Requirements:  req,
		Plan:          plan,
		Findings:      findings,
		Clarification: clarify,
	}, worktreeRef(worktree), mustRead)
}

func buildVerifierResumePrompt(
	req, plan, worktree string,
	mustRead []string,
	clarify string,
) string {
	return prompts.BuildVerifierResumePrompt(tasks.TaskPaths{
		Requirements:  req,
		Plan:          plan,
		Clarification: clarify,
	}, worktreeRef(worktree), mustRead)
}

func buildVerifierFixPrompt(
	plan, findings, worktree, clarify string,
) string {
	return verifierFixPrompt(plan, findings, worktree, clarify)
}
