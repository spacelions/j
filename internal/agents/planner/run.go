package planner

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/lifecycle"
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
//
// Resume vs fresh is inferred from the row's PlanResumeSession: a
// non-empty value means `j tasks resume-plan` (or any wrapper that
// preserved the session) — reuse the existing session and pick the
// resume-framing prompt. An empty value means a fresh / restart run
// (`j tasks start` or `j tasks re-plan`, the latter clears the
// session beforehand) — mint a new id via NewResumeID. Worker /
// verifier follow the same shape.
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
	clarificationPath := filepath.Join(
		taskDir, tasks.ClarificationFileName,
	)
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)

	existing, err := resolver.TaskByID(opts.TaskID)
	if err != nil {
		return err
	}

	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		uitheme.DangerousDialogBox(stderr, "J: %v", mustReadErr)
	}

	resumeMode := existing.PlanResumeSession != ""
	lc, resumeID := beginPlanLifecycle(
		ctx, opts, existing, stderr, agentLogPath, resumeMode,
	)
	resumeFromClarification := resumeMode &&
		tasks.ClarificationFileExists(taskDir)
	pid, planErr := opts.Agent.Plan(ctx, codingagents.PlanRequest{
		FromFilePath:            requirementsPath,
		Model:                   opts.Model,
		RequirementsOutputPath:  requirementsPath,
		PlanOutputPath:          planPath,
		ClarificationPath:       clarificationPath,
		Interactive:             opts.Interactive,
		ResumeChatID:            resumeID,
		Resume:                  resumeMode,
		ResumeFromClarification: resumeFromClarification,
		AgentLogPath:            agentLogPath,
		MustRead:                mustReadFiles,
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
			uitheme.DangerousDialogBox(
				stderr, "J: read %s: %v",
				requirementsPath, readErr,
			)
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

// beginPlanLifecycle picks the correct lifecycle helper given the
// inferred resume mode and the row's status. The returned resume id
// is what gets forwarded to the agent's PlanRequest.ResumeChatID:
// the row's stored value on resume, a freshly-minted one on fresh
// runs.
func beginPlanLifecycle(ctx context.Context, opts ExecuteOptions,
	existing tasks.Task, stderr io.Writer, agentLogPath string,
	resumeMode bool,
) (*lifecycle.PlanLifecycle, string) {
	if resumeMode {
		lc := lifecycle.BeginPlanResume(existing, stderr,
			opts.Agent.Name(), opts.Model, agentLogPath)
		return lc, existing.PlanResumeSession
	}
	resumeID, resumeErr := opts.Agent.NewResumeID(ctx)
	if resumeErr != nil {
		uitheme.DangerousDialogBox(stderr, "J: %v", resumeErr)
	}
	if existing.Status == tasks.StatusPlanning {
		lc := lifecycle.BeginPlanExisting(existing, stderr,
			opts.Agent.Name(), opts.Model, resumeID, agentLogPath)
		return lc, resumeID
	}
	lc := lifecycle.BeginPlanRestart(existing, stderr,
		opts.Agent.Name(), opts.Model, resumeID, agentLogPath)
	return lc, resumeID
}
