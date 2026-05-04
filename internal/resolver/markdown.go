package resolver

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/banner"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/util/mdfile"
	"github.com/spacelions/j/internal/util/run"
)

type PlanMarkdownOptions struct {
	RawTarget         string
	Source            PlanMarkdownSource
	Stdout            io.Writer
	Stderr            io.Writer
	Agent             codingagents.Agent
	Model             string
	Interactive       bool
	WaitForCompletion bool
}

type PlanMarkdownSource struct {
	Target string
	Body   string
}

func ResolvePlanMarkdown(rawTarget string) (PlanMarkdownSource, error) {
	target, err := mdfile.Resolve(rawTarget)
	if err != nil {
		return PlanMarkdownSource{}, err
	}
	body, err := os.ReadFile(target)
	if err != nil {
		return PlanMarkdownSource{}, fmt.Errorf("read source: %w", err)
	}
	return PlanMarkdownSource{Target: target, Body: string(body)}, nil
}

func RunPlanMarkdown(ctx context.Context, opts PlanMarkdownOptions) error {
	source := opts.Source
	if source.Target == "" {
		var err error
		source, err = ResolvePlanMarkdown(opts.RawTarget)
		if err != nil {
			return err
		}
	}
	taskID := store.NewTaskID()
	taskDir, err := store.EnsureTaskDir(taskID)
	if err != nil {
		return fmt.Errorf("plan: ensure task dir: %w", err)
	}
	return runPlanInTaskDir(ctx, opts, taskID, taskDir, source.Target, source.Body)
}

// RunPlanFromBody mirrors RunPlanMarkdown for the in-memory body
// flow (`j plan --from-linear`): it mints a task dir, stages the
// body as requirements.md so the coding agent has a real file to
// read, and runs the plan against that path. The agent reads
// requirements.md, refines it, and writes back — same shape as the
// re-plan flow. sourceLabel is recorded on the lifecycle row so
// `j tasks` can show the issue identifier instead of an empty
// source field.
func RunPlanFromBody(ctx context.Context, opts PlanMarkdownOptions, body, sourceLabel string) error {
	taskID := store.NewTaskID()
	taskDir, err := store.EnsureTaskDir(taskID)
	if err != nil {
		return fmt.Errorf("plan: ensure task dir: %w", err)
	}
	requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
	if err := os.WriteFile(requirementsPath, []byte(body), 0o644); err != nil {
		return fmt.Errorf("plan: stage requirements: %w", err)
	}
	if sourceLabel == "" {
		sourceLabel = requirementsPath
	}
	return runPlanInTaskDir(ctx, opts, taskID, taskDir, requirementsPath, body)
}

// runPlanInTaskDir is the shared lifecycle: build PlanRequest, drive
// agent.Plan, capture the refined requirements / plan, and finalize
// the task row. Both RunPlanMarkdown (real file) and RunPlanFromBody
// (in-memory body staged into requirements.md) feed through it so
// the lifecycle bookkeeping has exactly one source of truth.
func runPlanInTaskDir(ctx context.Context, opts PlanMarkdownOptions, taskID, taskDir, fromFilePath, sourceBody string) error {
	requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
	planPath := filepath.Join(taskDir, store.PlanFileName)

	resumeID, err := opts.Agent.NewResumeID(ctx)
	if err != nil {
		banner.DangerousFprintf(opts.Stderr, "J: warning: %v\n", err)
	}
	agentLogPath := filepath.Join(taskDir, store.AgentLogFileName)
	mustReadFiles, mustReadErr := MustRead()
	if mustReadErr != nil {
		banner.DangerousFprintf(opts.Stderr, "J: warning: %v\n", mustReadErr)
	}
	lc := store.NewPlanTask(opts.Stderr, opts.Agent.Name(), opts.Model, taskID, fromFilePath, sourceBody, resumeID)
	pid, planErr := opts.Agent.Plan(ctx, codingagents.PlanRequest{
		FromFilePath:           fromFilePath,
		Model:                  opts.Model,
		RequirementsOutputPath: requirementsPath,
		PlanOutputPath:         planPath,
		Interactive:            opts.Interactive,
		ResumeChatID:           resumeID,
		AgentLogPath:           agentLogPath,
		MustRead:               mustReadFiles,
	})

	if planErr == nil && pid > 0 {
		if opts.WaitForCompletion {
			if err := run.WaitForExit(ctx, pid); err != nil {
				lc.Finish(err, "", "", fromFilePath)
				return err
			}
		} else {
			lc.RecordBackground(pid, agentLogPath)
			banner.RunningInBackground(opts.Stdout, opts.Agent.Name(), pid, agentLogPath)
			return nil
		}
	}

	var refinedReq, planMD string
	if planErr == nil {
		if data, readErr := os.ReadFile(requirementsPath); readErr == nil {
			refinedReq = string(data)
		} else {
			banner.DangerousFprintf(opts.Stderr, "J: warning: read %s: %v\n", requirementsPath, readErr)
		}
		if data, readErr := os.ReadFile(planPath); readErr == nil {
			planMD = string(data)
		} else {
			banner.DangerousFprintf(opts.Stderr, "J: warning: read %s: %v\n", planPath, readErr)
		}
	}
	lc.Finish(planErr, refinedReq, planMD, fromFilePath)
	if planErr != nil {
		return planErr
	}

	banner.Fprintf(opts.Stdout, "J: the requirements.md and plan.md are saved in .j/tasks/%s/\n", taskID)
	return nil
}

type StartTarget struct {
	TaskID string
	IsNew  bool
	Body   string
	Source string
}

func NewStartTargetFromMarkdown(raw string) (StartTarget, error) {
	abs, err := mdfile.Resolve(raw)
	if err != nil {
		return StartTarget{}, err
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return StartTarget{}, fmt.Errorf("J: read source: %w", err)
	}
	return StartTarget{TaskID: store.NewTaskID(), IsNew: true, Body: string(body), Source: abs}, nil
}

// NewStartTargetFromBody mints an in-memory StartTarget for sources
// that don't have a markdown file on disk (the Linear flow).
// PrepareStartTaskFiles writes the body unchanged to
// `<taskDir>/requirements.md`. sourceLabel is recorded on the task
// row so `j tasks` can show the issue identifier instead of a path.
func NewStartTargetFromBody(body, sourceLabel string) StartTarget {
	return StartTarget{TaskID: store.NewTaskID(), IsNew: true, Body: body, Source: sourceLabel}
}

func PrepareStartTaskFiles(target StartTarget) (string, error) {
	taskDir, err := store.EnsureTaskDir(target.TaskID)
	if err != nil {
		return "", fmt.Errorf("J: ensure task dir: %w", err)
	}
	if target.IsNew {
		requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
		if err := os.WriteFile(requirementsPath, []byte(target.Body), 0o644); err != nil {
			return "", fmt.Errorf("J: stage requirements: %w", err)
		}
	}
	return filepath.Join(taskDir, store.AgentLogFileName), nil
}
