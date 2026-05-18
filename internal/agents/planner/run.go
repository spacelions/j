package planner

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
)

// Options configures Execute. Agent and Model must already be
// resolved by the caller (the shell-out branch of New does this via
// resolver.Agent before calling Execute).
type Options struct {
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
// resume-framing prompt. An empty value means a fresh / restart run;
// mint a new id via NewResumeID. Worker / verifier follow the same
// shape.
func Execute(ctx context.Context, opts Options) error {
	stderr := opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	res, err := resolver.ResolvePlanTask(opts.TaskID)
	if err != nil {
		return err
	}

	agent := opts.Agent
	session := beginPlanSession(ctx, opts, stderr, res.Task)
	lc := beginPlanLifecycle(res.Task, stderr, session)
	return runPlanner(ctx, agent, lc, res, session, opts)
}

func runPlanner(
	ctx context.Context,
	agent codingagents.Agent,
	lc *lifecycle.PlanLifecycle,
	res resolver.PlanTask,
	session codingagents.AgentSession,
	opts Options,
) error {
	stderr := opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		uitheme.DangerousDialogBox(stderr, "J: %v", mustReadErr)
	}
	resume := session.ResumeID == res.Task.PlanResumeSession &&
		session.ResumeID != ""
	resumeFromClarification := resume &&
		tasks.ClarificationFileExists(res.TaskDir)
	beginAt := time.Now().UTC()
	req := buildPlanRequest(res, session, opts.Interactive,
		resume, resumeFromClarification, mustReadFiles)
	pid, planErr := agent.Plan(ctx, req)
	capture := codingagents.ResumeCapture{
		TaskDir: res.TaskDir,
		Since:   beginAt,
		Stderr:  stderr,
	}

	if planErr == nil {
		resumeID, err := codingagents.CaptureAndSaveProcessResumeID(
			ctx, agent, lc, capture, codingagents.ResumeProcess{
				PID:      pid,
				Wait:     opts.WaitForCompletion,
				ResumeID: session.ResumeID,
			},
		)
		session.ResumeID = resumeID
		if err != nil {
			lc.Finish(err, "", "", res.Paths.Requirements)
			return err
		}
	}

	refinedReq, planMD := readPlanArtifacts(
		stderr, planErr, res.Paths.Requirements, res.Paths.Plan,
	)
	lc.Finish(planErr, refinedReq, planMD, res.Paths.Requirements)
	return planErr
}

// readPlanArtifacts reads the planner-produced requirements.md and
// plan.md when planErr is nil, surfacing read failures as warnings on
// stderr so the lifecycle still records what it can.
func readPlanArtifacts(
	stderr io.Writer, planErr error,
	requirementsPath, planPath string,
) (refinedReq, planMD string) {
	if planErr != nil {
		return "", ""
	}
	if data, readErr := os.ReadFile(requirementsPath); readErr == nil {
		refinedReq = string(data)
	} else if errors.Is(readErr, fs.ErrNotExist) {
		_, _ = uitheme.NormalFprintf(
			stderr, "J: missing planner artifact %s\n",
			requirementsPath,
		)
	} else {
		_, _ = uitheme.DangerousFprintf(
			stderr, "J: read %s: %v\n", requirementsPath, readErr,
		)
	}
	if data, readErr := os.ReadFile(planPath); readErr == nil {
		planMD = string(data)
	} else if errors.Is(readErr, fs.ErrNotExist) {
		_, _ = uitheme.NormalFprintf(
			stderr, "J: missing planner artifact %s\n", planPath,
		)
	} else {
		_, _ = uitheme.DangerousFprintf(
			stderr, "J: read %s: %v\n", planPath, readErr,
		)
	}
	return refinedReq, planMD
}

// beginPlanLifecycle picks the correct lifecycle helper given the
// inferred resume mode and the row's status. The returned resume id
// is what gets forwarded to the agent's PlanRequest.ResumeChatID:
// the row's stored value on resume, a freshly-minted one on fresh
// runs.
func beginPlanSession(
	ctx context.Context,
	opts Options,
	stderr io.Writer,
	existing tasks.Task,
) codingagents.AgentSession {
	if existing.PlanResumeSession != "" {
		return codingagents.AgentSession{
			Tool:     opts.Agent.Name(),
			Model:    opts.Model,
			ResumeID: existing.PlanResumeSession,
		}
	}
	resumeID, resumeErr := opts.Agent.NewResumeID(ctx)
	if resumeErr != nil {
		uitheme.DangerousDialogBox(stderr, "J: %v", resumeErr)
	}
	return codingagents.AgentSession{
		Tool:     opts.Agent.Name(),
		Model:    opts.Model,
		ResumeID: resumeID,
	}
}

func beginPlanLifecycle(
	existing tasks.Task,
	stderr io.Writer,
	session codingagents.AgentSession,
) *lifecycle.PlanLifecycle {
	if existing.PlanResumeSession != "" {
		return lifecycle.BeginPlanResume(existing, stderr, session)
	}
	if existing.Status == tasks.StatusPlanning {
		return lifecycle.BeginPlanExisting(existing, stderr, session)
	}
	return lifecycle.BeginPlanRestart(existing, stderr, session)
}

func buildPlanRequest(
	res resolver.PlanTask,
	session codingagents.AgentSession,
	interactive bool,
	resume bool,
	resumeFromClarification bool,
	mustRead []string,
) codingagents.PlanRequest {
	return codingagents.PlanRequest{
		TaskDir:                 res.TaskDir,
		FromFilePath:            res.Paths.Requirements,
		Model:                   session.Model,
		RequirementsOutputPath:  res.Paths.Requirements,
		PlanOutputPath:          res.Paths.Plan,
		ClarificationPath:       res.Paths.Clarification,
		Interactive:             interactive,
		ResumeChatID:            session.ResumeID,
		Resume:                  resume,
		ResumeFromClarification: resumeFromClarification,
		AgentLogPath: filepath.Join(
			res.TaskDir,
			tasks.AgentLogFileName,
		),
		MustRead: mustRead,
	}
}
