package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/util/run"
)

// UI is the narrow set of picker methods the worker functions call.
type UI interface {
	PickTask(ctx context.Context, title string, t []tasks.Task) (
		string, bool, error,
	)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
	ConfirmStatusOverride(
		ctx context.Context, cmd, taskID, status string,
	) (bool, error)
}

// ExecuteOptions configures Execute.
type ExecuteOptions struct {
	TaskID      string
	Yes         bool
	Interactive bool
	Tool        string
	Model       string
	// WaitForCompletion blocks on a returned PID and finishes synchronously.
	WaitForCompletion bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI

	// Store, when non-nil, receives writes recording tool/model/interactive.
	// When nil, each read/write opens <cwd>/.j/settings for its own duration.
	Store *store.Store
}

// Execute resolves a plan, picks a worker agent, and dispatches.
func Execute(ctx context.Context, opts ExecuteOptions) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("no coding agents configured")
	}
	res, ok, err := resolver.ResolveWorkPlan(ctx, resolver.WorkPlanOptions{
		TaskID: opts.TaskID,
		UI:     opts.UI,
	})
	if err != nil || !ok {
		return err
	}
	// A non-empty WorkResumeSession on the task row signals "resume"
	// — the row was last touched by a worker run that recorded its
	// session id. resume-work leaves it populated; re-work clears it
	// before re-execing so this branch picks the fresh path. In
	// resume mode the agent + model are pinned to the row's
	// WorkTool / WorkModel so claude is reused with `--resume <id>`
	// (resuming a claude session in cursor is impossible).
	resumeMode := res.Task.WorkResumeSession != ""
	agent, model, resumeID, err := resolveWorker(ctx, opts, res, resumeMode)
	if err != nil {
		return err
	}
	proceed, confirmErr := resolver.ConfirmStatusOverride(
		ctx, opts.UI, opts.Yes, "work", res.Task, resolver.ReplanAllowed,
	)
	if confirmErr != nil {
		return confirmErr
	}
	if !proceed {
		return nil
	}
	agentLogPath := filepath.Join(
		filepath.Dir(res.PlanPath), tasks.AgentLogFileName,
	)
	lc := beginWorkLifecycle(
		res, opts.Stderr, agent.Name(), model, resumeID,
		agentLogPath, resumeMode, opts.Interactive,
	)
	return runWorker(
		ctx, opts, agent, lc, res, model, resumeID, resumeMode, agentLogPath,
	)
}

// resolveWorker picks the agent + model + resume id for this run.
// On resume the row's WorkTool / WorkModel / WorkResumeSession are
// reused verbatim; on a fresh run resolver.Agent picks the bucket
// agent and NewResumeID mints the cursor id.
func resolveWorker(
	ctx context.Context, opts ExecuteOptions,
	res resolver.WorkPlan, resumeMode bool,
) (codingagents.Agent, string, string, error) {
	if resumeMode {
		a, ok := lookupResumeAgent(opts.Agents, res.Task.WorkTool)
		if !ok {
			return nil, "", "", fmt.Errorf(
				"unknown tool %q", res.Task.WorkTool,
			)
		}
		return a, res.Task.WorkModel, res.Task.WorkResumeSession, nil
	}
	a, m, err := selectWorker(ctx, opts)
	if err != nil {
		return nil, "", "", err
	}
	fresh, err := a.NewResumeID(ctx)
	if err != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", err)
	}
	return a, m, fresh, nil
}

func beginWorkLifecycle(
	res resolver.WorkPlan, stderr io.Writer,
	agentName, model, resumeID, agentLogPath string,
	resumeMode, interactive bool,
) *lifecycle.WorkLifecycle {
	if resumeMode {
		return lifecycle.BeginWorkResume(
			res.Task, stderr, agentLogPath, interactive,
		)
	}
	return lifecycle.BeginWorkRestart(
		res.Task, stderr, agentName, model, resumeID, agentLogPath,
		interactive,
	)
}

func runWorker(
	ctx context.Context, opts ExecuteOptions,
	agent codingagents.Agent, lc *lifecycle.WorkLifecycle,
	res resolver.WorkPlan, model, resumeID string,
	resumeMode bool, agentLogPath string,
) error {
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", mustReadErr)
	}
	taskDir := filepath.Dir(res.PlanPath)
	clarificationPath := filepath.Join(taskDir, tasks.ClarificationFileName)
	resumeFromClarification := resumeMode &&
		tasks.ClarificationFileExists(taskDir)
	workspace := taskDir
	beginAt := time.Now().UTC()
	pid, workErr := agent.Work(ctx, codingagents.WorkRequest{
		PlanPath:                res.PlanPath,
		Model:                   model,
		ClarificationPath:       clarificationPath,
		Interactive:             opts.Interactive,
		ResumeChatID:            resumeID,
		Resume:                  resumeMode,
		ResumeFromClarification: resumeFromClarification,
		Worktree:                lc.Task().Worktree,
		AgentLogPath:            agentLogPath,
		MustRead:                mustReadFiles,
	})
	if workErr == nil && pid > 0 {
		if opts.WaitForCompletion {
			if err := run.WaitForExit(ctx, pid); err != nil {
				lc.Finish(err)
				return err
			}
		} else {
			lc.RecordAgentLog(agentLogPath)
			uitheme.NormalForkDialog(
				opts.Stdout, agent.Name(), pid, agentLogPath,
			)
			return nil
		}
	}
	if workErr == nil && resumeID == "" {
		codingagents.CaptureAndRecordResume(
			ctx, agent, lc, workspace, beginAt, opts.Stderr,
		)
	}
	lc.Finish(workErr)
	if workErr != nil {
		return workErr
	}
	uitheme.NormalFprintf(
		opts.Stdout, "J: working on task %s\n", res.Task.ID,
	)
	return nil
}

// lookupResumeAgent finds the agent in agents whose Name matches the
// task row's recorded WorkTool. Returns false when the row was written
// by a backend the current binary no longer ships (an empty WorkTool
// also fails so the caller surfaces a clear error rather than silently
// running with the first agent).
func lookupResumeAgent(
	agents []codingagents.Agent, name string,
) (codingagents.Agent, bool) {
	if name == "" {
		return nil, false
	}
	for _, a := range agents {
		if a.Name() == name {
			return a, true
		}
	}
	return nil, false
}

func selectWorker(
	ctx context.Context, opts ExecuteOptions,
) (codingagents.Agent, string, error) {
	return resolver.Agent(ctx, resolver.AgentOptions{
		Bucket:        store.BucketWorker,
		Agents:        opts.Agents,
		ExplicitTool:  opts.Tool,
		ExplicitModel: opts.Model,
		UI:            opts.UI,
		Store:         opts.Store,
		Stderr:        opts.Stderr,
		Interactive:   opts.Interactive,
	})
}

func (o ExecuteOptions) withDefaults() ExecuteOptions {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.UI == nil {
		o.UI = picker.New(o.Stdin, o.Stderr)
	}
	return o
}
