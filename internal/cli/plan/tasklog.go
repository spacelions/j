package plan

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// planLifecycle owns the begin/end task-log writes around a single
// agent.Plan invocation. The struct holds no bbolt handle — every
// task-log write goes through persistTaskWarn (defined in resume.go),
// which opens `<cwd>/.j/tasks/list.db`, writes, and closes within the
// same call so the bbolt file lock is never held across agent.Plan
// and a concurrent `j tasks` from another shell is not blocked. The
// lifecycle is constructed with beginPlanTask and finalised with
// finishPlan; callers pair them with a defer so the task is always
// written even when agent.Plan panics.
type planLifecycle struct {
	stderr io.Writer
	task   store.Task
	closed bool
}

// beginPlanTask records the "planning" entry for a real plan run.
//   - taskID: minted by the caller so the per-task directory under
//     <cwd>/.j/tasks/ uses the same id as the bbolt row.
//   - target: the markdown file the user is planning against. The
//     summary is derived from the body when possible and falls back
//     to the file basename so `j tasks` never shows a blank summary.
//   - requirement: the markdown body the user is planning from.
//   - planResumeChatID: per-phase resume token from
//     Agent.NewResumeID; empty for agents with no notion of resume
//     or on a NewResumeID failure (already warned by the caller).
//
// Best effort: failure to open the task log or to write the initial
// row warns once on stderr and execution continues. The on-disk
// pre-flight (`j init`) has already laid down `.j/tasks/list.db`, so
// the open call is read/write only and never creates new files.
func beginPlanTask(opts Options, agent codingagents.Agent, model, taskID, target, requirement, planResumeChatID string) *planLifecycle {
	begin := time.Now().UTC()
	task := store.Task{
		ID:               taskID,
		Status:           store.StatusPlanning,
		InvokedTool:      agent.Name(),
		InvokedModel:     model,
		PlanResumeCursor: planResumeChatID,
		Summary:          planSummary(requirement, target),
		PlanBeginAt:      &begin,
	}
	lc := &planLifecycle{stderr: opts.Stderr, task: task}
	persistTaskWarn(opts.Stderr, task)
	return lc
}

// openTaskLog opens `<cwd>/.j/tasks/list.db` for the plan lifecycle.
// Like openSettingsStore in plan.go this is the post-init replacement
// for store.OpenTaskLog: pre-flight ensures the file exists, so any
// failure here surfaces as a single "warning: ..." line on stderr.
// Callers that just want the open-write-close pattern should use
// persistTaskWarn instead. Both helpers share the same shape so a
// future consolidation does not break callers.
func openTaskLog(stderr io.Writer) (*store.Store, bool) {
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		fmt.Fprintf(stderr, "warning: tasks path: %v\n", err)
		return nil, false
	}
	s, err := store.Open(path)
	if err != nil {
		fmt.Fprintf(stderr, "warning: tasks db: %v\n", err)
		return nil, false
	}
	return s, true
}

// finishPlan stamps plan_end_at, decides the terminal status from
// runErr, and (when planErr is nil) re-derives Summary from the
// refined requirements (then the plan body, then the file basename)
// because the agent may have rewritten the requirements during the
// session. The task is rewritten to the log even on errors so `help`
// is observable from `j tasks`. The bbolt store is opened just long
// enough to write the row and closed before this returns; calling
// finishPlan twice is a silent no-op via the closed flag.
func (lc *planLifecycle) finishPlan(runErr error, refinedRequirements, planMarkdown, target string) {
	if lc.closed {
		return
	}
	lc.closed = true
	end := time.Now().UTC()
	lc.task.PlanEndAt = &end
	if runErr != nil {
		lc.task.Status = store.StatusHelp
	} else {
		lc.task.Status = store.StatusPlanDone
		lc.task.Summary = planSummary(pickSummarySource(refinedRequirements, planMarkdown), target)
	}
	persistTaskWarn(lc.stderr, lc.task)
}

// pickSummarySource returns whichever of the refined requirements or
// the plan body has a usable first non-empty line, preferring the
// requirements summary because that is the document the agent rewrote
// to capture user intent. Both empty falls through to the file
// basename in planSummary.
func pickSummarySource(refinedRequirements, planMarkdown string) string {
	if store.SummarizeMarkdown(refinedRequirements) != "" {
		return refinedRequirements
	}
	return planMarkdown
}

// planSummary picks a one-line summary in this order:
//  1. first non-empty line of the requirement / plan markdown,
//  2. the requirement file basename when the body was unreadable.
//
// Truncation is delegated to store.SummarizeMarkdown for the body
// path; the basename path is short by construction.
func planSummary(requirement, target string) string {
	if s := store.SummarizeMarkdown(requirement); s != "" {
		return s
	}
	if target != "" {
		return filepath.Base(target)
	}
	return ""
}
