package resolver

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store/tasks"
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

// PlanMarkdownSource carries the resolved planning source. LinearIssue
// is non-empty only when the source originated from a Linear issue;
// it is recorded on the task row so `j tasks` can surface the
// upstream identifier and so re-plans round-trip the value.
type PlanMarkdownSource struct {
	Target      string
	Body        string
	LinearIssue string
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

// TODO: this should be moved to cli package and use cli/uitheme for user-facing messages,
// but for now this is a convenient place to put the core logic of the command without
// depending on the CLI package.
func RunPlanMarkdown(ctx context.Context, opts PlanMarkdownOptions) error {
	source := opts.Source
	if source.Target == "" {
		var err error
		source, err = ResolvePlanMarkdown(opts.RawTarget)
		if err != nil {
			return err
		}
	}
	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		return fmt.Errorf("plan: ensure task dir: %w", err)
	}
	return runPlanInTaskDir(ctx, opts, taskID, taskDir, source.Target, source.Body, source.LinearIssue)
}

// RunPlanFromBody mirrors RunPlanMarkdown for the in-memory body
// flow (`j plan --from-linear`): it mints a task dir, stages the
// body as requirements.md so the coding agent has a real file to
// read, and runs the plan against that path. The agent reads
// requirements.md, refines it, and writes back — same shape as the
// re-plan flow. sourceLabel is recorded on the lifecycle row so
// `j tasks` can show the issue identifier instead of an empty
// source field. linearIssue is the upstream `<TEAM>-<NUM>` form
// stamped on the task row's linear_issue field; empty for non-Linear
// sources.
func RunPlanFromBody(ctx context.Context, opts PlanMarkdownOptions, body, sourceLabel, linearIssue string) error {
	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		return fmt.Errorf("plan: ensure task dir: %w", err)
	}
	requirementsPath := filepath.Join(taskDir, tasks.RequirementsFileName)
	if err := os.WriteFile(requirementsPath, []byte(body), 0o644); err != nil {
		return fmt.Errorf("plan: stage requirements: %w", err)
	}
	return runPlanInTaskDir(ctx, opts, taskID, taskDir, requirementsPath, body, linearIssue)
}

// runPlanInTaskDir is the shared lifecycle: build PlanRequest, drive
// agent.Plan, capture the refined requirements / plan, and finalize
// the task row. Both RunPlanMarkdown (real file) and RunPlanFromBody
// (in-memory body staged into requirements.md) feed through it so
// the lifecycle bookkeeping has exactly one source of truth.
func runPlanInTaskDir(ctx context.Context, opts PlanMarkdownOptions, taskID, taskDir, fromFilePath, sourceBody, linearIssue string) error {
	requirementsPath := filepath.Join(taskDir, tasks.RequirementsFileName)
	planPath := filepath.Join(taskDir, tasks.PlanFileName)

	resumeID, err := opts.Agent.NewResumeID(ctx)
	if err != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", err)
	}
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)
	mustReadFiles, mustReadErr := MustRead()
	if mustReadErr != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", mustReadErr)
	}
	lc := lifecycle.NewPlanTask(opts.Stderr, opts.Agent.Name(), opts.Model, taskID, fromFilePath, sourceBody, resumeID, agentLogPath, linearIssue)
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
			uitheme.NormalForkDialog(opts.Stdout, opts.Agent.Name(), pid, agentLogPath)
			return nil
		}
	}

	var refinedReq, planMD string
	if planErr == nil {
		if data, readErr := os.ReadFile(requirementsPath); readErr == nil {
			refinedReq = string(data)
		} else {
			uitheme.DangerousDialogBox(opts.Stderr, "J: read %s: %v", requirementsPath, readErr)
		}
		if data, readErr := os.ReadFile(planPath); readErr == nil {
			planMD = string(data)
		} else {
			uitheme.DangerousDialogBox(opts.Stderr, "J: read %s: %v", planPath, readErr)
		}
	}
	lc.Finish(planErr, refinedReq, planMD, fromFilePath)
	if planErr != nil {
		return planErr
	}

	uitheme.NormalFprintf(opts.Stdout, "J: the requirements.md and plan.md are saved in .j/tasks/%s/\n", taskID)
	return nil
}

// StartTarget bundles the resolved input for `j tasks start`. IsNew
// signals that requirements.md still needs to be written (the markdown
// or Linear arms); existing-task arms leave it false. LinearIssue is
// the upstream identifier (`ENG-123`) when the source was Linear, so
// the lifecycle can stamp it onto the task row.
type StartTarget struct {
	TaskID      string
	IsNew       bool
	Body        string
	Source      string
	LinearIssue string
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
	return StartTarget{TaskID: tasks.NewTaskID(), IsNew: true, Body: string(body), Source: abs}, nil
}

// NewStartTargetFromBody mints an in-memory StartTarget for sources
// that don't have a markdown file on disk (the Linear flow).
// PrepareStartTaskFiles writes the body unchanged to
// `<taskDir>/requirements.md`. sourceLabel is recorded on the task
// row so `j tasks` can show the issue identifier instead of a path.
// linearIssue is the upstream `<TEAM>-<NUM>` form, propagated into
// the row's linear_issue column.
func NewStartTargetFromBody(body, sourceLabel, linearIssue string) StartTarget {
	return StartTarget{TaskID: tasks.NewTaskID(), IsNew: true, Body: body, Source: sourceLabel, LinearIssue: linearIssue}
}

func PrepareStartTaskFiles(target StartTarget) (string, error) {
	taskDir, err := tasks.EnsureDir(target.TaskID)
	if err != nil {
		return "", fmt.Errorf("J: ensure task dir: %w", err)
	}
	if target.IsNew {
		requirementsPath := filepath.Join(taskDir, tasks.RequirementsFileName)
		if err := os.WriteFile(requirementsPath, []byte(target.Body), 0o644); err != nil {
			return "", fmt.Errorf("J: stage requirements: %w", err)
		}
	}
	return filepath.Join(taskDir, tasks.AgentLogFileName), nil
}
