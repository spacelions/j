package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	PickTask(ctx context.Context, title string, t []tasks.Task) (string, bool, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
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

// Execute resolves a plan, selects a worker agent, and hands the plan to the agent.
func Execute(ctx context.Context, opts ExecuteOptions) (err error) {
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
	// A non-empty WorkResumeSession on the task row signals "resume"
	// — the row was last touched by a worker run that recorded its
	// session id. resume-work leaves it populated; re-work clears it
	// before re-execing so this branch picks the fresh path. In
	// resume mode the agent + model are pinned to the row's
	// WorkTool / WorkModel so claude is reused with `--resume <id>`
	// (resuming a claude session in cursor is impossible).
	resumeMode := res.Task.WorkResumeSession != ""
	var (
		agent codingagents.Agent
		model string
	)
	resumeID := res.Task.WorkResumeSession
	if resumeMode {
		a, ok := lookupResumeAgent(opts.Agents, res.Task.WorkTool)
		if !ok {
			return fmt.Errorf("J: unknown tool %q", res.Task.WorkTool)
		}
		agent = a
		model = res.Task.WorkModel
	} else {
		a, m, err := selectWorker(ctx, opts)
		if err != nil {
			return err
		}
		agent = a
		model = m
		fresh, err := agent.NewResumeID(ctx)
		if err != nil {
			uitheme.DangerousDialogBox(opts.Stderr, "J: %v", err)
		}
		resumeID = fresh
	}
	proceed, confirmErr := resolver.ConfirmStatusOverride(ctx, opts.UI, opts.Yes, "work", res.Task, resolver.ReplanAllowed)
	if confirmErr != nil {
		return confirmErr
	}
	if !proceed {
		return nil
	}
	agentLogPath := filepath.Join(filepath.Dir(res.PlanPath), tasks.AgentLogFileName)
	var lc *lifecycle.WorkLifecycle
	if resumeMode {
		lc = lifecycle.BeginWorkResume(res.Task, opts.Stderr, agentLogPath)
	} else {
		lc = lifecycle.BeginWorkReuse(res.Task, opts.Stderr, agent.Name(), model, resumeID, agentLogPath)
	}

	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", mustReadErr)
	}
	pid, workErr := agent.Work(ctx, codingagents.WorkRequest{
		PlanPath:     res.PlanPath,
		Model:        model,
		Interactive:  opts.Interactive,
		ResumeChatID: resumeID,
		Resume:       resumeMode,
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
	uitheme.NormalFprintf(opts.Stdout, "J: working on task %s\n", res.Task.ID)
	return nil
}

// lookupResumeAgent finds the agent in agents whose Name matches the
// task row's recorded WorkTool. Returns false when the row was written
// by a backend the current binary no longer ships (an empty WorkTool
// also fails so the caller surfaces a clear error rather than silently
// running with the first agent).
func lookupResumeAgent(agents []codingagents.Agent, name string) (codingagents.Agent, bool) {
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

func selectWorker(ctx context.Context, opts ExecuteOptions) (codingagents.Agent, string, error) {
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
