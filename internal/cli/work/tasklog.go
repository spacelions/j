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
// no-ops; the lifecycle is still safe to call.
//
// The struct is constructed with one of beginWorkTaskNew or
// beginWorkTaskReuse depending on whether the run is a legacy file
// import (creates a new bbolt row) or a bbolt-sourced run (mutates an
// existing row in place). finishWork is shared.
type workLifecycle struct {
	stderr io.Writer
	store  *store.Store
	task   store.Task
	closed bool
}

// beginWorkTaskNew records the "working" entry for a legacy
// `--from-file` import. The caller has already minted the task id and
// staged the plan markdown into <cwd>/.j/tasks/<id>/plan.md (and
// optionally requirements.md). This helper just stamps the bbolt row.
func beginWorkTaskNew(opts Options, agent codingagents.Agent, model, taskID, planPath, requirement, planBody, workResumeChatID string) *workLifecycle {
	begin := time.Now().UTC()
	task := store.Task{
		ID:               taskID,
		Status:           store.StatusWorking,
		InvokedTool:      agent.Name(),
		InvokedModel:     model,
		WorkResumeCursor: workResumeChatID,
		Summary:          workSummary(requirement, planBody, planPath),
		WorkBeginAt:      &begin,
	}
	return openLifecycle(opts, task)
}

// beginWorkTaskReuse mutates a copy of `existing` to flip status to
// `working`, stamp work_begin_at, clear stale work_end_at / done_at
// from a previous failed run, and record the latest tool/model and
// resume cursor for the work phase. Plan-phase fields are preserved.
func beginWorkTaskReuse(opts Options, agent codingagents.Agent, model string, existing store.Task, workResumeChatID string) *workLifecycle {
	begin := time.Now().UTC()
	task := existing
	task.Status = store.StatusWorking
	task.InvokedTool = agent.Name()
	task.InvokedModel = model
	task.WorkResumeCursor = workResumeChatID
	task.WorkBeginAt = &begin
	task.WorkEndAt = nil
	task.DoneAt = nil
	return openLifecycle(opts, task)
}

// openLifecycle is the shared helper that opens the task log,
// best-effort writes the initial row, and returns a workLifecycle
// suitable for finishWork. Open / put failures warn once and then
// degrade to a nil-store lifecycle so the orchestrator can still run.
func openLifecycle(opts Options, task store.Task) *workLifecycle {
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
// (work-done on success, help on error), and rewrites the task. The
// `completed` status (and DoneAt) is reserved for a future `j verify`
// command; `j work` no longer terminates the lifecycle here. Closing
// the store is idempotent so callers can safely defer this even when
// finishWork already ran on the happy path.
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
		lc.task.Status = store.StatusWorkDone
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
// markdown from a plan path produced by `j plan`'s legacy
// `<dir>/<stem>.plan.md` convention and returns its contents when
// readable. When the plan path does not follow this convention, or
// the sidecar file does not exist / cannot be read, an empty string
// is returned so the caller falls back to the plan body for the
// summary.
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
// second, file basename last.
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
	return ""
}
