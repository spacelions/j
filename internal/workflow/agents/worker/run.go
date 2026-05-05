package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"

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

// ResumeOptions configures RunResume.
type ResumeOptions struct {
	TaskID string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI
}

func (o ResumeOptions) withDefaults() ResumeOptions {
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

// RunResume resumes a previously started work session. The task's existing
// WorkResumeSession is reused. Resume always runs interactive.
func RunResume(ctx context.Context, opts ResumeOptions) (err error) {
	defer func() {
		if errors.Is(err, huh.ErrUserAborted) {
			err = nil
		}
	}()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("J: no coding agents configured")
	}
	t, ok, err := resolveResumeTask(ctx, opts)
	if err != nil {
		return err
	}
	if !ok {
		uitheme.NormalFprintln(opts.Stdout, "J: there are no resumable sessions")
		return nil
	}
	agent, ok := lookupResumeAgent(opts.Agents, t.WorkTool)
	if !ok {
		return fmt.Errorf("J: unknown tool %q", t.WorkTool)
	}
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		return err
	}
	taskDir := filepath.Join(tasksDir, t.ID)
	planPath := filepath.Join(taskDir, tasks.PlanFileName)
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)

	lc := t.BeginWorkResume(opts.Stderr, agentLogPath)
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", mustReadErr)
	}
	_, workErr := agent.Work(ctx, codingagents.WorkRequest{
		PlanPath:     planPath,
		Model:        t.WorkModel,
		Interactive:  true,
		ResumeChatID: t.WorkResumeSession,
		Resume:       true,
		MustRead:     mustReadFiles,
	})
	lc.Finish(workErr)
	if workErr != nil {
		return workErr
	}
	uitheme.NormalFprintf(opts.Stdout, "J: work resume on task %s\n", t.ID)
	return nil
}

func resolveResumeTask(ctx context.Context, opts ResumeOptions) (tasks.Task, bool, error) {
	if opts.TaskID != "" {
		return resolveResumeByID(opts.TaskID)
	}
	rows, err := listResumableTasks()
	if err != nil {
		return tasks.Task{}, false, err
	}
	switch len(rows) {
	case 0:
		return tasks.Task{}, false, nil
	case 1:
		return rows[0], true, nil
	}
	chosen, ok, err := opts.UI.PickTask(ctx, "Select a task to resume", rows)
	if err != nil {
		return tasks.Task{}, false, err
	}
	if !ok {
		return tasks.Task{}, false, nil
	}
	for _, t := range rows {
		if t.ID == chosen {
			return t, true, nil
		}
	}
	return tasks.Task{}, false, fmt.Errorf("J: task %q not found", chosen)
}

func resolveResumeByID(id string) (tasks.Task, bool, error) {
	t, err := resolver.TaskByID(id)
	if err != nil {
		return tasks.Task{}, false, err
	}
	if t.WorkResumeSession == "" {
		return tasks.Task{}, false, fmt.Errorf("J: task %q has no work session", id)
	}
	return t, true, nil
}

func listResumableTasks() ([]tasks.Task, error) {
	all, err := resolver.ListAllTasks()
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, t := range all {
		if t.WorkResumeSession != "" {
			out = append(out, t)
		}
	}
	return out, nil
}

func lookupResumeAgent(agents []codingagents.Agent, tool string) (codingagents.Agent, bool) {
	for _, a := range agents {
		if a.Name() == tool {
			return a, true
		}
	}
	return nil, false
}
