package testcases_test

import (
	"io"

	"github.com/spacelions/j/internal/lifecycle"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
)

func newPlanLifecycle(
	stderr io.Writer,
	agentName, model, taskID, target string,
	requirement, resumeID, _ string,
	linearIssue string,
) *lifecycle.PlanLifecycle {
	return lifecycle.NewPlanTask(stderr, taskID, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	}, lifecycle.PlanSource{
		Requirement: requirement,
		Target:      target,
		LinearIssue: linearIssue,
	})
}

func newWorkLifecycle(
	stderr io.Writer,
	agentName, model, taskID, planPath string,
	requirement, planBody, resumeID, _ string,
) *lifecycle.WorkLifecycle {
	return lifecycle.NewWorkTask(stderr, taskID, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	}, lifecycle.WorkSource{
		Requirement: requirement,
		PlanBody:    planBody,
		PlanPath:    planPath,
	})
}

func beginWorkRestartLifecycle(
	t tasks.Task,
	stderr io.Writer,
	agentName, model, resumeID, _ string,
) *lifecycle.WorkLifecycle {
	return lifecycle.BeginWorkRestart(t, stderr, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	})
}

func beginVerifyRestartLifecycle(
	t tasks.Task,
	stderr io.Writer,
	agentName, model, resumeID, _ string,
) *lifecycle.VerifyLifecycle {
	return lifecycle.BeginVerifyRestart(t, stderr, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	})
}
