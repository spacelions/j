package orchestrator

import (
	"io"

	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// TaskContext groups values shared by every phase in one task run.
type TaskContext struct {
	MaxIterations int
	TaskID        string
	Agents        []codingagents.Agent
	Stderr        io.Writer
}

// PhaseConfig groups the phase-specific run options.
type PhaseConfig struct {
	Phase                RunPhase
	PlanRequiresApproval bool
	Overrides            PhaseOverrides
	Tagger               func(phase string)
}

func newTaskContext(
	maxIterations int,
	taskID string,
	agents []codingagents.Agent,
	stderr io.Writer,
) TaskContext {
	return TaskContext{
		MaxIterations: maxIterations,
		TaskID:        taskID,
		Agents:        agents,
		Stderr:        stderr,
	}
}
