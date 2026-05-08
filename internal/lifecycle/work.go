package lifecycle

import (
	"context"
	"io"
	"time"

	"github.com/spacelions/j/internal/lifecycle/tuiquit"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// prDetectTimeout bounds the work-end PR-URL detection (matches
// the gh-fallback budget in tuiquit).
const prDetectTimeout = 5 * time.Second

// WorkLifecycle owns the begin/end task-log writes around a single
// agent.Work invocation. Mirrors PlanLifecycle.
type WorkLifecycle struct {
	stderr       io.Writer
	agentLogPath string
	task         tasks.Task
	closed       bool
}

// NewWorkTask records the "working" entry for a newly created work
// row. The caller has already minted the task id and staged the
// plan markdown into <cwd>/.j/tasks/<id>/plan.md.
func NewWorkTask(stderr io.Writer, agentName, model, taskID,
	planPath, requirement, planBody, resumeID, agentLogPath string,
) *WorkLifecycle {
	task := tasks.Task{
		ID:                taskID,
		Status:            tasks.StatusPlanDone,
		WorkTool:          agentName,
		WorkModel:         model,
		WorkResumeSession: resumeID,
		Summary: tasks.FromPlanAndRequirement(
			requirement, planBody, planPath),
		WorkBeginAt: time.Now().UTC(),
	}
	fillWorktree(&task)
	return openWorkLifecycle(stderr, task, agentLogPath,
		tasks.EventWorkBegin, "work begin")
}

// BeginWorkRestart mutates a copy of t to flip status to `working`.
func BeginWorkRestart(t tasks.Task, stderr io.Writer, agentName, model,
	resumeID, agentLogPath string,
) *WorkLifecycle {
	task := t
	task.WorkTool = agentName
	task.WorkModel = model
	task.WorkResumeSession = resumeID
	task.WorkBeginAt = time.Now().UTC()
	task.WorkEndAt = time.Time{}
	task.DoneAt = time.Time{}
	fillWorktree(&task)
	return openWorkLifecycle(stderr, task, agentLogPath,
		tasks.EventWorkRestart, "work restart")
}

// BeginWorkResume is the resume-flow companion of BeginWorkRestart.
func BeginWorkResume(t tasks.Task, stderr io.Writer,
	agentLogPath string,
) *WorkLifecycle {
	task := t
	task.WorkEndAt = time.Time{}
	task.DoneAt = time.Time{}
	if task.WorkBeginAt.IsZero() {
		task.WorkBeginAt = time.Now().UTC()
	}
	return openWorkLifecycle(stderr, task, agentLogPath,
		tasks.EventWorkResume, "work resume")
}

func openWorkLifecycle(stderr io.Writer, task tasks.Task,
	agentLogPath string, ev tasks.Event, panicTag string,
) *WorkLifecycle {
	task.AgentLogPath = agentLogPath
	if _, err := tasks.ApplyAndPersistWarn(
		stderr, &task, ev); err != nil {
		panic(panicTag + ": " + err.Error())
	}
	return &WorkLifecycle{
		stderr:       stderr,
		agentLogPath: agentLogPath,
		task:         task,
	}
}

func fillWorktree(task *tasks.Task) {
	if task.Worktree != "" {
		return
	}
	project, _ := store.ProjectName()
	task.Worktree = tasks.WorktreeNameFor(project, *task)
}

// RecordBackground stamps the spawned child's PID and the agent log
// path on the in-memory work task row and re-persists it.
func (lc *WorkLifecycle) RecordBackground(pid int, logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.BackgroundPID = pid
	lc.task.AgentLogPath = logPath
	tasks.PersistWarn(lc.stderr, lc.task)
}

// Finish stamps work_end_at, picks the terminal status from runErr.
func (lc *WorkLifecycle) Finish(runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.WorkEndAt = time.Now().UTC()
	lc.detectPullRequestURL()

	ev := tasks.EventWorkDone
	if runErr != nil {
		ev = tasks.EventWorkError
	}
	if _, err := tasks.ApplyAndPersistWarn(
		lc.stderr, &lc.task, ev); err != nil {
		panic("work finish: " + err.Error())
	}
}

func (lc *WorkLifecycle) detectPullRequestURL() {
	if lc.task.PullRequestURL != "" {
		return
	}
	ctx, cancel := context.WithTimeout(
		context.Background(), prDetectTimeout)
	defer cancel()
	url := tuiquit.DetectPullRequestURL(
		ctx, lc.task.Worktree, lc.agentLogPath)
	if url != "" {
		lc.task.PullRequestURL = url
	}
}

// Task returns the in-memory snapshot of the work task row.
func (lc *WorkLifecycle) Task() tasks.Task { return lc.task }
