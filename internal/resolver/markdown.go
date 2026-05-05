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
	requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
	planPath := filepath.Join(taskDir, store.PlanFileName)

	resumeID, err := opts.Agent.NewResumeID(ctx)
	if err != nil {
		banner.DangerousBox(opts.Stderr, "J: %v", err)
	}
	agentLogPath := filepath.Join(taskDir, store.AgentLogFileName)
	mustReadFiles, mustReadErr := MustRead()
	if mustReadErr != nil {
		banner.DangerousBox(opts.Stderr, "J: %v", mustReadErr)
	}
	lc := store.NewPlanTask(opts.Stderr, opts.Agent.Name(), opts.Model, taskID, source.Target, source.Body, resumeID)
	pid, planErr := opts.Agent.Plan(ctx, codingagents.PlanRequest{
		FromFilePath:           source.Target,
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
				lc.Finish(err, "", "", source.Target)
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
			banner.DangerousBox(opts.Stderr, "J: read %s: %v", requirementsPath, readErr)
		}
		if data, readErr := os.ReadFile(planPath); readErr == nil {
			planMD = string(data)
		} else {
			banner.DangerousBox(opts.Stderr, "J: read %s: %v", planPath, readErr)
		}
	}
	lc.Finish(planErr, refinedReq, planMD, source.Target)
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
