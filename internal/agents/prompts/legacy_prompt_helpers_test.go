package prompts

import "github.com/spacelions/j/internal/store/tasks"

func BuildPlanner(targetPath string, mustRead []string) string {
	return BuildPlannerPrompt(targetPath, mustRead)
}

func BuildPlannerResume(targetPath string, mustRead []string) string {
	return BuildPlannerResumePrompt(targetPath, mustRead)
}

func BuildPlannerClarificationResume(
	targetPath, clarificationPath string,
	mustRead []string,
) string {
	return BuildPlannerClarificationResumePrompt(
		targetPath,
		clarificationPath,
		mustRead,
	)
}

func BuildWorker(
	planPath, worktree string,
	mustRead []string,
	clarificationPath string,
) string {
	return BuildWorkerPrompt(tasks.TaskPaths{
		Plan:          planPath,
		Clarification: clarificationPath,
	}, worktree, mustRead)
}

func BuildWorkerResume(
	planPath, worktree string,
	mustRead []string,
	clarificationPath string,
) string {
	return BuildWorkerResumePrompt(tasks.TaskPaths{
		Plan:          planPath,
		Clarification: clarificationPath,
	}, worktree, mustRead)
}

func BuildWorkerClarificationResume(
	planPath, worktree string,
	mustRead []string,
	clarificationPath string,
) string {
	return BuildWorkerClarificationResumePrompt(tasks.TaskPaths{
		Plan:          planPath,
		Clarification: clarificationPath,
	}, worktree, mustRead)
}

func BuildVerifier(
	reqPath, planPath, _ string,
	findingsPath, worktree string,
	mustRead []string,
	clarificationPath string,
) string {
	return BuildVerifierPrompt(tasks.TaskPaths{
		Requirements:  reqPath,
		Plan:          planPath,
		Findings:      findingsPath,
		Clarification: clarificationPath,
	}, worktree, mustRead)
}

func BuildVerifierResume(
	reqPath, planPath, worktree string,
	mustRead []string,
	clarificationPath string,
) string {
	return BuildVerifierResumePrompt(tasks.TaskPaths{
		Requirements:  reqPath,
		Plan:          planPath,
		Clarification: clarificationPath,
	}, worktree, mustRead)
}

func BuildVerifierClarificationResume(
	reqPath, planPath, worktree string,
	mustRead []string,
	clarificationPath string,
) string {
	return BuildVerifierClarificationResumePrompt(tasks.TaskPaths{
		Requirements:  reqPath,
		Plan:          planPath,
		Clarification: clarificationPath,
	}, worktree, mustRead)
}

func BuildVerifierFix(
	planPath, findingsPath, worktree, clarificationPath string,
) string {
	return BuildVerifierFixPrompt(tasks.TaskPaths{
		Plan:          planPath,
		Findings:      findingsPath,
		Clarification: clarificationPath,
	}, worktree)
}
