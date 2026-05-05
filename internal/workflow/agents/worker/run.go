package worker

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/util/run"
)

// UI is the narrow set of picker methods the worker functions call.
type UI interface {
	PickTask(ctx context.Context, title string, t []tasks.Task) (string, bool, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
}

// Options configures Run.
type Options struct {
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

// Run resolves a plan, selects a worker agent, and hands the plan to the agent.
func Run(ctx context.Context, opts Options) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("J: no coding agents configured")
	}
	res, ok, err := resolver.ResolveWorkPlan(ctx, resolver.WorkPlanOptions{
		TaskID: opts.TaskID,
		UI:     opts.UI,
	})
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	agent, model, err := selectWorker(ctx, opts)
	if err != nil {
		return err
	}
	resumeID, err := agent.NewResumeID(ctx)
	if err != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", err)
	}
	proceed, confirmErr := resolver.ConfirmStatusOverride(ctx, opts.UI, opts.Yes, "work", res.Task, resolver.ReplanAllowed)
	if confirmErr != nil {
		return confirmErr
	}
	if !proceed {
		return nil
	}
	agentLogPath := filepath.Join(filepath.Dir(res.PlanPath), tasks.AgentLogFileName)
	lc := res.Task.BeginWorkReuse(opts.Stderr, agent.Name(), model, resumeID, agentLogPath)

	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", mustReadErr)
	}
	pid, workErr := agent.Work(ctx, codingagents.WorkRequest{
		PlanPath:     res.PlanPath,
		Model:        model,
		Interactive:  opts.Interactive,
		ResumeChatID: resumeID,
		Worktree:     lc.Task().Worktree,
		AgentLogPath: agentLogPath,
		MustRead:     mustReadFiles,
	})
	if workErr == nil && pid > 0 {
		if opts.WaitForCompletion {
			if err := run.WaitForExit(ctx, pid); err != nil {
				lc.Finish(err)
				return err
			}
		} else {
			lc.RecordBackground(pid, agentLogPath)
			uitheme.NormalForkDialog(opts.Stdout, agent.Name(), pid, agentLogPath)
			return nil
		}
	}
	lc.Finish(workErr)
	if workErr != nil {
		return workErr
	}
	uitheme.NormalFprintf(opts.Stdout, "J: coding on task %s\n", res.Task.ID)
	return nil
}

func selectWorker(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
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

func (o Options) withDefaults() Options {
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
