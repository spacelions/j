package planner

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/util/run"
)

// ExecuteOptions configures Execute. Agent and Model must already be
// resolved by the caller (the shell-out branch of New does this via
// resolver.Agent before calling Execute).
type ExecuteOptions struct {
	TaskID            string
	Agent             codingagents.Agent
	Model             string
	Interactive       bool
	WaitForCompletion bool
	Stderr            io.Writer
}

// Execute runs the planner phase against an already-seeded task. It
// loads the task row, drives agent.Plan, and finalises the lifecycle
// row. WaitForCompletion must be true in the orchestrator chain so
// the next phase (worker) starts only after the planner exits.
func Execute(ctx context.Context, opts ExecuteOptions) error {
	stderr := opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		return err
	}
	taskDir := filepath.Join(tasksDir, opts.TaskID)
	requirementsPath := filepath.Join(taskDir, tasks.RequirementsFileName)
	planPath := filepath.Join(taskDir, tasks.PlanFileName)
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)

	existing, err := resolver.TaskByID(opts.TaskID)
	if err != nil {
		return err
	}

	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		uitheme.DangerousDialogBox(stderr, "J: %v", mustReadErr)
	}

	resumeID, resumeErr := opts.Agent.NewResumeID(ctx)
	if resumeErr != nil {
		uitheme.DangerousDialogBox(stderr, "J: %v", resumeErr)
	}

	lc := existing.BeginPlanReuse(stderr, opts.Agent.Name(), opts.Model, resumeID, agentLogPath)
	pid, planErr := opts.Agent.Plan(ctx, codingagents.PlanRequest{
		FromFilePath:           requirementsPath,
		Model:                  opts.Model,
		RequirementsOutputPath: requirementsPath,
		PlanOutputPath:         planPath,
		Interactive:            opts.Interactive,
		ResumeChatID:           resumeID,
		AgentLogPath:           agentLogPath,
		MustRead:               mustReadFiles,
	})

	if planErr == nil && pid > 0 && opts.WaitForCompletion {
		if err := run.WaitForExit(ctx, pid); err != nil {
			lc.Finish(err, "", "", requirementsPath)
			return err
		}
	}

	var refinedReq, planMD string
	if planErr == nil {
		if data, readErr := os.ReadFile(requirementsPath); readErr == nil {
			refinedReq = string(data)
		} else {
			uitheme.DangerousDialogBox(stderr, "J: read %s: %v", requirementsPath, readErr)
		}
		if data, readErr := os.ReadFile(planPath); readErr == nil {
			planMD = string(data)
		} else {
			uitheme.DangerousDialogBox(stderr, "J: read %s: %v", planPath, readErr)
		}
	}
	lc.Finish(planErr, refinedReq, planMD, requirementsPath)
	return planErr
}
