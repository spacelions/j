package plan

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// planLifecycle owns the begin/end task-log writes around a single
// agent.Plan invocation. A nil store field means OpenTaskLog failed
// (already warned by the store helper) and subsequent updates are
// silent no-ops. The struct is constructed with beginPlanTask and
// finalised with finishPlan; callers pair them with a defer so the
// task is always written even when agent.Plan panics.
type planLifecycle struct {
	stderr io.Writer
	store  *store.Store
	task   store.Task
	closed bool
}

// beginPlanTask records the "planning" entry for a real plan run.
//   - target: the markdown file the user is planning against, or
//     "" for a scratch session.
//   - requirement: the markdown body the user is planning from, or
//     "" for scratch. The summary is derived from the body when
//     possible and falls back to the file basename, then to a
//     stable label so `j tasks` never shows a blank summary.
//
// Best effort: failure to open the task log or to write the initial
// row warns once on stderr (the store helper handles open warnings;
// the put error is wrapped here) and execution continues with a
// nil-store lifecycle.
func beginPlanTask(opts Options, agent codingagents.Agent, model, target, requirement string) *planLifecycle {
	begin := time.Now().UTC()
	task := store.Task{
		ID:                  store.NewTaskID(),
		RequirementMarkdown: requirement,
		Status:              store.StatusPlanning,
		InvokedTool:         agent.Name(),
		InvokedModel:        model,
		ResumeCursor:        planResumeCursor(target),
		Summary:             planSummary(requirement, target),
		PlanBeginAt:         &begin,
	}
	lc := &planLifecycle{stderr: opts.Stderr, task: task}
	s, ok := store.OpenTaskLog(opts.Stderr, store.BucketTasks)
	if !ok {
		return lc
	}
	lc.store = s
	if err := s.PutTask(task); err != nil {
		fmt.Fprintf(opts.Stderr, "warning: tasks put: %v\n", err)
	}
	return lc
}

// finishPlan stamps plan_end_at, decides the terminal status from
// runErr, and (when planErr is nil) attaches the plan markdown body
// passed in by the caller. The task is rewritten to the log even on
// errors so `help` is observable from `j tasks`. The store is closed
// here, idempotently.
func (lc *planLifecycle) finishPlan(runErr error, planMarkdown string) {
	if lc.closed {
		return
	}
	lc.closed = true
	end := time.Now().UTC()
	lc.task.PlanEndAt = &end
	if runErr != nil {
		lc.task.Status = store.StatusHelp
	} else {
		lc.task.Status = store.StatusPlanned
		if planMarkdown != "" {
			pm := planMarkdown
			lc.task.PlanMarkdown = &pm
		}
	}
	if lc.store == nil {
		return
	}
	if err := lc.store.PutTask(lc.task); err != nil {
		fmt.Fprintf(lc.stderr, "warning: tasks put: %v\n", err)
	}
	_ = lc.store.Close()
}

// planSummary picks a one-line summary in this order:
//  1. first non-empty line of the requirement markdown,
//  2. the requirement file basename when the body was unreadable,
//  3. a constant scratch-session label so the row is still findable.
//
// Truncation is delegated to store.SummarizeMarkdown for the body
// path; the basename / label paths are short by construction.
func planSummary(requirement, target string) string {
	if s := store.SummarizeMarkdown(requirement); s != "" {
		return s
	}
	if target != "" {
		return filepath.Base(target)
	}
	return "from scratch"
}

// planResumeCursor returns a workspace path the user can later feed to
// cursor-agent to pick up where they left off. For a markdown source
// it's the directory containing the target (matching the agent's own
// --workspace argument); for a scratch session there's no target so we
// fall back to the cwd, and finally to "" if even that is unavailable.
func planResumeCursor(target string) string {
	if target != "" {
		return codingagents.DefaultWorkspace(target)
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return ""
}
