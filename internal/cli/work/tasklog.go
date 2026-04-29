package work

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// workLifecycle owns the begin/end task-log writes around a single
// agent.Work invocation. A nil store means OpenTaskLog failed (already
// warned by the store helper) and the per-event updates become silent
// no-ops; the lifecycle is still safe to call. Construction goes
// through beginWorkTask; finishWork stamps work_end_at, decides the
// terminal status, and closes the store idempotently.
type workLifecycle struct {
	stderr io.Writer
	store  *store.Store
	task   store.Task
	closed bool
}

// beginWorkTask records the "working" entry. The plan body the agent
// will execute is the canonical PlanMarkdown. The original requirement
// markdown is recovered from the sidecar `<stem>.md` next to the plan
// when present (the standard `j plan` output convention); otherwise it
// stays empty. The summary mirrors the same precedence used by `j plan`
// so a task started by `j work` looks consistent with one started by
// `j plan`.
func beginWorkTask(opts Options, agent codingagents.Agent, model, planPath, planBody string) *workLifecycle {
	begin := time.Now().UTC()
	requirement := readRequirementSidecar(planPath)
	planMD := planBody
	task := store.Task{
		ID:                  store.NewTaskID(),
		RequirementMarkdown: requirement,
		PlanMarkdown:        &planMD,
		Status:              store.StatusWorking,
		InvokedTool:         agent.Name(),
		InvokedModel:        model,
		ResumeCursor:        codingagents.DefaultWorkspace(planPath),
		Summary:             workSummary(requirement, planBody, planPath),
		WorkBeginAt:         &begin,
	}
	lc := &workLifecycle{stderr: opts.Stderr, task: task}
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

// finishWork stamps work_end_at, picks the terminal status from runErr
// (done on success, help on error), sets done_at on success, and
// rewrites the task. Closing the store is idempotent so callers can
// safely defer this even when finishWork already ran on the happy
// path.
func (lc *workLifecycle) finishWork(runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	end := time.Now().UTC()
	lc.task.WorkEndAt = &end
	if runErr != nil {
		lc.task.Status = store.StatusHelp
	} else {
		lc.task.Status = store.StatusDone
		lc.task.DoneAt = &end
	}
	if lc.store == nil {
		return
	}
	if err := lc.store.PutTask(lc.task); err != nil {
		fmt.Fprintf(lc.stderr, "warning: tasks put: %v\n", err)
	}
	_ = lc.store.Close()
}

// readRequirementSidecar derives the path to the original requirement
// markdown from a plan path produced by `j plan` and returns its
// contents when readable. The convention is `<dir>/<stem>.plan.md` for
// the plan and `<dir>/<stem>.md` for the requirement. When the plan
// path does not follow this convention, or the sidecar file does not
// exist / cannot be read, an empty string is returned and Task.
// RequirementMarkdown is left empty.
func readRequirementSidecar(planPath string) string {
	if planPath == "" {
		return ""
	}
	base := filepath.Base(planPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	stem = strings.TrimSuffix(stem, ".plan")
	if stem == "" {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(planPath), stem+".md")
	if candidate == planPath {
		return ""
	}
	data, err := os.ReadFile(candidate)
	if err != nil {
		return ""
	}
	return string(data)
}

// workSummary mirrors planSummary's precedence so the column lines up
// across plan- and work-initiated tasks: requirement first, plan body
// second, file basename last. The constant fallback ("work session")
// keeps the row searchable even when the agent was invoked against a
// completely empty plan.
func workSummary(requirement, planBody, planPath string) string {
	if s := store.SummarizeMarkdown(requirement); s != "" {
		return s
	}
	if s := store.SummarizeMarkdown(planBody); s != "" {
		return s
	}
	if planPath != "" {
		return filepath.Base(planPath)
	}
	return "work session"
}
