package lifecycle

import (
	"io"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
)

func newPlanTaskTest(
	stderr io.Writer,
	agentName, model, taskID, target string,
	requirement, resumeID, agentLogPath string,
	linearIssue string,
) *PlanLifecycle {
	lc := NewPlanTask(stderr, taskID, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	}, PlanSource{
		Requirement: requirement,
		Target:      target,
		LinearIssue: linearIssue,
	})
	setPlanLogPath(stderr, lc, agentLogPath, tasks.EventPlanBegin)
	return lc
}

func beginPlanRestartTest(
	t tasks.Task,
	stderr io.Writer,
	agentName, model, resumeID, agentLogPath string,
) *PlanLifecycle {
	lc := BeginPlanRestart(t, stderr, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	})
	setPlanLogPath(stderr, lc, agentLogPath, tasks.EventPlanRestart)
	return lc
}

func beginPlanResumeTest(
	t tasks.Task,
	stderr io.Writer,
	agentName, model, agentLogPath string,
) *PlanLifecycle {
	lc := BeginPlanResume(t, stderr, codingagents.AgentSession{
		Tool:  agentName,
		Model: model,
	})
	setPlanLogPath(stderr, lc, agentLogPath, tasks.EventPlanResume)
	return lc
}

func beginPlanExistingTest(
	t tasks.Task,
	stderr io.Writer,
	agentName, model, resumeID, agentLogPath string,
) *PlanLifecycle {
	lc := BeginPlanExisting(t, stderr, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	})
	setPlanLogPath(stderr, lc, agentLogPath, tasks.EventPlanBegin)
	return lc
}

func newWorkTaskTest(
	stderr io.Writer,
	agentName, model, taskID, planPath string,
	requirement, planBody, resumeID, agentLogPath string,
) *WorkLifecycle {
	lc := NewWorkTask(stderr, taskID, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	}, WorkSource{
		Requirement: requirement,
		PlanBody:    planBody,
		PlanPath:    planPath,
	})
	setWorkLogPath(stderr, lc, agentLogPath, tasks.EventWorkBegin)
	return lc
}

func beginWorkRestartTest(
	t tasks.Task,
	stderr io.Writer,
	agentName, model, resumeID, agentLogPath string,
) *WorkLifecycle {
	lc := BeginWorkRestart(t, stderr, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	})
	setWorkLogPath(stderr, lc, agentLogPath, tasks.EventWorkRestart)
	return lc
}

func beginWorkResumeTest(
	t tasks.Task,
	stderr io.Writer,
	agentLogPath string,
) *WorkLifecycle {
	lc := BeginWorkResume(t, stderr)
	setWorkLogPath(stderr, lc, agentLogPath, tasks.EventWorkResume)
	return lc
}

func beginVerifyRestartTest(
	t tasks.Task,
	stderr io.Writer,
	agentName, model, resumeID, agentLogPath string,
) *VerifyLifecycle {
	lc := BeginVerifyRestart(t, stderr, codingagents.AgentSession{
		Tool:     agentName,
		Model:    model,
		ResumeID: resumeID,
	})
	setVerifyLogPath(stderr, lc, agentLogPath, tasks.EventVerifyBegin)
	return lc
}

func beginVerifyResumeTest(
	t tasks.Task,
	stderr io.Writer,
	agentLogPath string,
) *VerifyLifecycle {
	lc := BeginVerifyResume(t, stderr)
	setVerifyLogPath(stderr, lc, agentLogPath, tasks.EventVerifyResume)
	return lc
}

func setPlanLogPath(
	stderr io.Writer,
	lc *PlanLifecycle,
	path string,
	ev tasks.Event,
) {
	lc.agentLogPath = path
	lc.task.AgentLogPath = path
	tasks.PersistWarn(stderr, lc.task)
	if path != "" {
		markersHook(tasks.Transition{Event: ev}, lc.task)
	}
}

func setWorkLogPath(
	stderr io.Writer,
	lc *WorkLifecycle,
	path string,
	ev tasks.Event,
) {
	lc.agentLogPath = path
	lc.task.AgentLogPath = path
	tasks.PersistWarn(stderr, lc.task)
	if path != "" {
		markersHook(tasks.Transition{Event: ev}, lc.task)
	}
}

func setVerifyLogPath(
	stderr io.Writer,
	lc *VerifyLifecycle,
	path string,
	ev tasks.Event,
) {
	lc.agentLogPath = path
	lc.task.AgentLogPath = path
	tasks.PersistWarn(stderr, lc.task)
	if path != "" {
		markersHook(tasks.Transition{Event: ev}, lc.task)
	}
}
