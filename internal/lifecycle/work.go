package lifecycle

import (
	"context"
	"io"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/lifecycle/tuiquit"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// prDetectTimeout bounds the work-end PR-URL detection (matches
// the gh-fallback budget in tuiquit).
const prDetectTimeout = 5 * time.Second

type WorkSource struct {
	Requirement string
	PlanBody    string
	PlanPath    string
}

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
func NewWorkTask(
	stderr io.Writer,
	taskID string,
	session codingagents.AgentSession,
	src WorkSource,
) *WorkLifecycle {
	task := tasks.Task{
		ID:                taskID,
		Status:            tasks.StatusPlanDone,
		WorkTool:          session.Tool,
		WorkModel:         session.Model,
		WorkResumeSession: session.ResumeID,
		Summary: tasks.FromPlanAndRequirement(
			src.Requirement, src.PlanBody, src.PlanPath),
		WorkBeginAt: time.Now().UTC(),
	}
	fillWorktree(&task)
	return openWorkLifecycle(stderr, task, taskAgentLogPath(taskID),
		tasks.EventWorkBegin, "work begin")
}

// BeginWorkRestart mutates a copy of t to flip status to `working`.
func BeginWorkRestart(
	t tasks.Task,
	stderr io.Writer,
	session codingagents.AgentSession,
) *WorkLifecycle {
	task := t
	task.WorkTool = session.Tool
	task.WorkModel = session.Model
	task.WorkResumeSession = session.ResumeID
	task.WorkBeginAt = time.Now().UTC()
	task.WorkEndAt = time.Time{}
	task.DoneAt = time.Time{}
	fillWorktree(&task)
	return openWorkLifecycle(stderr, task, taskAgentLogPath(t.ID),
		tasks.EventWorkRestart, "work restart")
}

// BeginWorkResume is the resume-flow companion of BeginWorkRestart.
func BeginWorkResume(t tasks.Task, stderr io.Writer) *WorkLifecycle {
	task := t
	task.WorkEndAt = time.Time{}
	task.DoneAt = time.Time{}
	if task.WorkBeginAt.IsZero() {
		task.WorkBeginAt = time.Now().UTC()
	}
	return openWorkLifecycle(stderr, task, taskAgentLogPath(t.ID),
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
	task.Worktree = tasks.WorktreeNameFor(store.ProjectName(), *task)
}

// RecordResumeSession stamps id onto the in-memory work task row's
// WorkResumeSession field and re-persists. See PlanLifecycle's
// equivalent for the post-run-capture rationale; deepseek-tui mints
// the session id after its first turn writes to disk.
func (lc *WorkLifecycle) RecordResumeSession(id string) {
	if id == "" {
		return
	}
	lc.task.WorkResumeSession = id
	tasks.PersistWarn(lc.stderr, lc.task)
}

// RecordAgentLog stamps the per-task agent.log path on the in-memory
// work task row and re-persists it. See PlanLifecycle.RecordAgentLog
// for the SPA-72 rationale; the pid argument was dropped because the
// per-task `flock` is now the source of truth for liveness.
func (lc *WorkLifecycle) RecordAgentLog(logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.AgentLogPath = logPath
	tasks.PersistWarn(lc.stderr, lc.task)
}

// Finish stamps work_end_at and decides the terminal event from
// runErr and the on-disk clarification.md (mirroring PlanLifecycle).
// runErr always wins over the clarification check; a clean run that
// left a clarification.md routes to `needs-clarification` so the
// orchestrator skips the verifier and the user can answer the
// question before a resume.
func (lc *WorkLifecycle) Finish(runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.WorkEndAt = time.Now().UTC()
	lc.detectPullRequestURL()

	ev := lc.pickFinishEvent(runErr)
	if _, err := tasks.ApplyAndPersistWarn(
		lc.stderr, &lc.task, ev); err != nil {
		panic("work finish: " + err.Error())
	}
}

// pickFinishEvent decides which event drives the work-finish
// transition. Error path takes precedence over the clarification
// check, matching PlanLifecycle.pickFinishEvent's contract.
func (lc *WorkLifecycle) pickFinishEvent(runErr error) tasks.Event {
	if runErr != nil {
		return tasks.EventWorkError
	}
	if taskClarificationPresent(lc.task.ID) {
		return tasks.EventWorkNeedsClarification
	}
	return tasks.EventWorkDone
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
