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

// Options configures Execute.
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

// Execute resolves a plan, picks a worker agent, and dispatches.
func Execute(ctx context.Context, opts Options) (err error) {
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
	// session id. In resume mode the agent + model are pinned to the
	// row's WorkTool / WorkModel so claude is reused with `--resume <id>`
	// (resuming a claude session in cursor is impossible).
	agent, session, err := resolveWorker(ctx, opts, res)
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
	lc := beginWorkLifecycle(res, opts.Stderr, session)
	return runWorker(ctx, agent, lc, res, session, opts)
}

// resolveWorker picks the agent + model + resume id for this run.
// On resume the row's WorkTool / WorkModel / WorkResumeSession are
// reused verbatim; on a fresh run resolver.Agent picks the bucket
// agent and NewResumeID mints the cursor id.
func resolveWorker(
	ctx context.Context,
	opts Options,
	res resolver.WorkPlan,
) (codingagents.Agent, codingagents.AgentSession, error) {
	if res.Task.WorkResumeSession != "" {
		a, ok := lookupResumeAgent(opts.Agents, res.Task.WorkTool)
		if !ok {
			return nil, codingagents.AgentSession{}, fmt.Errorf(
				"unknown tool %q", res.Task.WorkTool,
			)
		}
		return a, codingagents.AgentSession{
			Tool:     res.Task.WorkTool,
			Model:    res.Task.WorkModel,
			ResumeID: res.Task.WorkResumeSession,
		}, nil
	}
	a, m, err := selectWorker(ctx, opts)
	if err != nil {
		return nil, codingagents.AgentSession{}, err
	}
	fresh, err := a.NewResumeID(ctx)
	if err != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", err)
	}
	return a, codingagents.AgentSession{
		Tool:     a.Name(),
		Model:    m,
		ResumeID: fresh,
	}, nil
}

func beginWorkLifecycle(
	res resolver.WorkPlan, stderr io.Writer,
	session codingagents.AgentSession,
) *lifecycle.WorkLifecycle {
	if res.Task.WorkResumeSession != "" {
		return lifecycle.BeginWorkResume(res.Task, stderr)
	}
	return lifecycle.BeginWorkRestart(res.Task, stderr, session)
}

func runWorker(
	ctx context.Context,
	agent codingagents.Agent, lc *lifecycle.WorkLifecycle,
	res resolver.WorkPlan,
	session codingagents.AgentSession,
	opts Options,
) error {
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", mustReadErr)
	}
	resume := res.Task.WorkResumeSession != ""
	resumeFromClarification := resume &&
		tasks.ClarificationFileExists(res.TaskDir)
	beginAt := time.Now().UTC()
	req := buildWorkRequest(res, session, opts.Interactive,
		resumeFromClarification, mustReadFiles)
	pid, workErr := agent.Work(ctx, req)
	if workErr == nil && pid > 0 {
		session.ResumeID = captureSpawnedWorkerResume(
			ctx, agent, lc, codingagents.ResumeCapture{
				TaskDir: res.TaskDir,
				Since:   beginAt,
				Stderr:  opts.Stderr,
			}, session.ResumeID, pid,
		)
		done, err := handleSpawnedWorker(ctx, agent, lc, req, opts, pid)
		if err != nil || done {
			return err
		}
	}
	if workErr == nil && session.ResumeID == "" {
		codingagents.CaptureAndRecordResume(
			ctx, agent, lc, codingagents.ResumeCapture{
				TaskDir: res.TaskDir,
				Since:   beginAt,
				Stderr:  opts.Stderr,
			},
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

func handleSpawnedWorker(
	ctx context.Context,
	agent codingagents.Agent,
	lc *lifecycle.WorkLifecycle,
	req codingagents.WorkRequest,
	opts Options,
	pid int,
) (bool, error) {
	if opts.WaitForCompletion {
		if err := run.WaitForExit(ctx, pid); err != nil {
			lc.Finish(err)
			return false, err
		}
		return false, nil
	}
	lc.RecordAgentLog(req.AgentLogPath)
	uitheme.NormalForkDialog(
		opts.Stdout, agent.Name(), pid, req.AgentLogPath,
	)
	return true, nil
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
	ctx context.Context, opts Options,
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

func buildWorkRequest(
	res resolver.WorkPlan,
	session codingagents.AgentSession,
	interactive bool,
	resumeFromClarification bool,
	mustRead []string,
) codingagents.WorkRequest {
	resume := session.ResumeID != "" &&
		session.ResumeID == res.Task.WorkResumeSession
	return codingagents.WorkRequest{
		TaskDir:                 res.TaskDir,
		PlanPath:                res.Paths.Plan,
		Model:                   session.Model,
		ClarificationPath:       res.Paths.Clarification,
		Interactive:             interactive,
		ResumeChatID:            session.ResumeID,
		Resume:                  resume,
		ResumeFromClarification: resumeFromClarification,
		Worktree:                res.Task.Worktree,
		AgentLogPath: filepath.Join(
			res.TaskDir,
			tasks.AgentLogFileName,
		),
		MustRead: mustRead,
	}
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
