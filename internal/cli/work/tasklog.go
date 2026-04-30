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
// agent.Work invocation. The struct holds no bbolt handle — every
// task-log write goes through writeWorkTaskWarn, which opens
// `<cwd>/.j/tasks/list.db`, writes, and closes within the same call
// so the bbolt file lock is never held across agent.Work and a
// concurrent `j tasks` from another shell is not blocked.
//
// The struct is constructed with one of beginWorkTaskNew,
// beginWorkTaskReuse, or beginWorkTaskResume depending on whether the
// run is a legacy file import (creates a new bbolt row), a
// bbolt-sourced run (mutates an existing row in place), or a resume.
// finishWork is shared.
type workLifecycle struct {
	stderr io.Writer
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

// beginWorkTaskResume is the resume-flow companion of
// beginWorkTaskReuse. The two functions diverge in two places:
//
//  1. The existing WorkResumeCursor is preserved verbatim instead
//     of being overwritten with a fresh `Agent.NewResumeID` value
//     (the whole point of resume is reusing the cursor recorded on
//     the task row).
//  2. The original WorkBeginAt timestamp is preserved when set so
//     the task row keeps its first-run lineage; only WorkEndAt /
//     DoneAt are cleared so finishWork stamps fresh values on the
//     next finalize. Tool/model are kept verbatim because resume
//     never re-prompts the user for them.
func beginWorkTaskResume(opts Options, existing store.Task) *workLifecycle {
	task := existing
	task.Status = store.StatusWorking
	task.WorkEndAt = nil
	task.DoneAt = nil
	if task.WorkBeginAt == nil {
		begin := time.Now().UTC()
		task.WorkBeginAt = &begin
	}
	return openLifecycle(opts, task)
}

// openLifecycle is the shared helper that best-effort writes the
// initial row and returns a workLifecycle suitable for finishWork.
// The bbolt handle is opened, written to, and closed within
// writeWorkTaskWarn so the file lock is not held across agent.Work.
// Pre-flight has already laid down `.j/tasks/list.db`, so the open
// call is read/write only.
func openLifecycle(opts Options, task store.Task) *workLifecycle {
	lc := &workLifecycle{stderr: opts.Stderr, task: task}
	writeWorkTaskWarn(opts.Stderr, task)
	return lc
}

// finishWork stamps work_end_at, picks the terminal status from runErr
// (work-done on success, help on error), and rewrites the task. The
// `completed` status (and DoneAt) is reserved for a future `j verify`
// command; `j work` no longer terminates the lifecycle here. The
// bbolt store is opened just long enough to write the row and closed
// before this returns; calling finishWork twice is a silent no-op
// via the closed flag.
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
	writeWorkTaskWarn(lc.stderr, lc.task)
}

// writeWorkTaskWarn opens `<cwd>/.j/tasks/list.db`, writes task, and
// closes the store. Open and put failures each surface as a single
// `warning: ...` line on stderr (open via openTaskLog, put inline)
// and the helper returns; persistence is best-effort by design.
// Designed to be called twice per work run — once at begin, once at
// finish — so the bbolt file lock is never held across agent.Work.
// Mirrors the persistTaskWarn pattern used by `j plan resume`.
func writeWorkTaskWarn(stderr io.Writer, task store.Task) {
	s, ok := openTaskLog(stderr)
	if !ok {
		return
	}
	defer func() { _ = s.Close() }()
	if err := s.PutTask(task); err != nil {
		fmt.Fprintf(stderr, "warning: tasks put: %v\n", err)
	}
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
