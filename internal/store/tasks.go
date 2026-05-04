package store

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	bolt "go.etcd.io/bbolt"
)

// AgentLogFileName is the per-task file that captures stdout/stderr
// of a fire-and-forget headless coding-agent child. It lives at
// `<cwd>/.j/tasks/<id>/agent.log` and is written to each task row's
// AgentLogPath so `j tasks` and the user can find it later. All
// phases (plan / work / verify) share this filename so the reaper in
// `j tasks` can surface the log regardless of which command spawned
// the child.
const AgentLogFileName = "agent.log"

// BucketTasks is the bucket inside the per-project task log DB
// (`<cwd>/.j/tasks`) that holds JSON-encoded Task values keyed by
// Task.ID. It is shared by the writers in `j plan` / `j work` and the
// reader in `j tasks`.
const BucketTasks = "tasks"

// VerifierPlanFileName is the filename of the verifier's draft
// verification plan stored under `<cwd>/.j/tasks/<id>/`. Written by
// `j verify` (via the agent's tool calls) and read by `j tasks`
// summary derivation.
const VerifierPlanFileName = "verifier_plan.md"

// VerifierFindingsFileName is the filename of the verifier's findings
// markdown stored under `<cwd>/.j/tasks/<id>/`. Its last non-empty
// line is parsed by the orchestrator into a PASS/FAIL verdict.
const VerifierFindingsFileName = "verifier_findings.md"

// summaryMaxRunes is the upper bound applied to Task.Summary in
// SummarizeMarkdown. Eighty runes fits a typical terminal column even
// after the ID/status/tool/model prefix, and pinning the value keeps
// the truncation behaviour tested in one place.
const summaryMaxRunes = 80

// TaskStatus is the typed string used in Task.Status. Only the values
// listed below are valid; Valid() is the allowlist guard used by
// PutTask so a misspelled enum can never reach disk.
type TaskStatus string

// Allowed Task.Status values. The set is intentionally closed: callers
// must use one of these constants. New states require a code change so
// the listing/sorting logic in `j tasks` can be updated together.
//
// `verifying`, `verify-done`, and `completed` are reserved for a future
// `j verify` command and are never written by `j plan` or `j work`
// today; the data model still includes them so listing/sorting code
// does not need to change when verification is wired up.
const (
	StatusPlanning   TaskStatus = "planning"
	StatusPlanDone   TaskStatus = "plan-done"
	StatusWorking    TaskStatus = "working"
	StatusWorkDone   TaskStatus = "work-done"
	StatusVerifying  TaskStatus = "verifying"
	StatusVerifyDone TaskStatus = "verify-done"
	StatusCompleted  TaskStatus = "completed"
	StatusHelp       TaskStatus = "help"
)

// Valid reports whether s is one of the allowlisted TaskStatus values.
func (s TaskStatus) Valid() bool {
	switch s {
	case StatusPlanning, StatusPlanDone, StatusWorking, StatusWorkDone,
		StatusVerifying, StatusVerifyDone, StatusCompleted, StatusHelp:
		return true
	}
	return false
}

// Task is the JSON-serialized value stored under each key in
// BucketTasks. Pointer-typed time fields are omitted when unknown so
// a partially-completed task (planning in flight, work not started)
// never claims fake timestamps.
//
// The body markdown is no longer stored in bbolt; the canonical
// requirement and plan documents live as files at
// `<cwd>/.j/tasks/<id>/requirements.md` and
// `<cwd>/.j/tasks/<id>/plan.md`. Bbolt only carries metadata.
//
// Each phase mints its own resume token via Agent.NewResumeID; an
// empty string means "no session for that phase yet".
type Task struct {
	ID           string     `json:"id"`
	Status       TaskStatus `json:"status"`
	InvokedTool  string     `json:"invoked_tool"`
	InvokedModel string     `json:"invoked_model"`
	// Worktree is the bare git-worktree name (no slashes, no path)
	// the worker and verifier should operate against for this task.
	// It is minted by `j work` on first run via WorktreeNameFor and
	// then preserved on every subsequent transition. Empty for
	// tasks worked before the field was introduced; downstream
	// agents treat empty as "fall back to the main checkout".
	Worktree string `json:"worktree,omitempty"`
	Summary  string `json:"summary"`

	PlanResumeCursor   string `json:"plan_resume_cursor"`
	WorkResumeCursor   string `json:"work_resume_cursor"`
	VerifyResumeCursor string `json:"verify_resume_cursor"`

	PlanBeginAt   *time.Time `json:"plan_begin_at,omitempty"`
	PlanEndAt     *time.Time `json:"plan_end_at,omitempty"`
	WorkBeginAt   *time.Time `json:"work_begin_at,omitempty"`
	WorkEndAt     *time.Time `json:"work_end_at,omitempty"`
	VerifyBeginAt *time.Time `json:"verify_begin_at,omitempty"`
	VerifyEndAt   *time.Time `json:"verify_end_at,omitempty"`
	DoneAt        *time.Time `json:"done_at,omitempty"`

	// BackgroundPID is the OS process id of the detached coding-agent
	// child spawned for a fire-and-forget headless `j plan` or `j work`
	// run. It is non-zero only while the row is in flight (planning or
	// working) and the child has not yet been reaped by `j tasks`.
	// Foreground (interactive) and resume runs leave it at 0.
	BackgroundPID int `json:"background_pid,omitempty"`
	// AgentLogPath is the absolute path of the per-task log file that
	// captures the spawned child's stdout/stderr (typically
	// `<cwd>/.j/tasks/<id>/agent.log`). It is set whenever a
	// background spawn was attempted so users can follow a backgrounded
	// run; the reaper does not clear it after the row is finalised so
	// the trailing log remains discoverable.
	AgentLogPath string `json:"agent_log_path,omitempty"`
}

// ulidEntropy is the process-local monotonic entropy source feeding
// NewTaskID. Wrapping crypto/rand.Reader in ulid.Monotonic guarantees
// strict lexicographic ordering for IDs minted within the same
// millisecond. The reader is not safe for concurrent use, so callers
// must hold ulidMu while invoking ulid.MustNew.
var (
	ulidMu      sync.Mutex
	ulidEntropy = ulid.Monotonic(rand.Reader, 0)
)

// NewTaskID returns a stable, unique, lexicographically time-sortable
// task identifier in canonical ULID form: 26 ASCII characters in
// Crockford base32 (`0-9A-HJKMNP-TV-Z`), where the leading 10 chars
// encode time.Now().UTC() at millisecond precision and the trailing
// 16 chars encode 80 bits of randomness from crypto/rand. IDs minted
// inside the same millisecond are strictly ascending thanks to the
// monotonic entropy source. The function is safe for concurrent use.
func NewTaskID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy).String()
}

// PutTask JSON-encodes t and writes it under t.ID in BucketTasks. The
// bucket is created if missing so callers do not need to call
// EnsureBucket. The status allowlist is enforced here so a misspelled
// enum surfaces as a deterministic error instead of corrupting the
// listing logic downstream.
func (s *Store) PutTask(t Task) error {
	if t.ID == "" {
		return errors.New("store: task id required")
	}
	if !t.Status.Valid() {
		return fmt.Errorf("store: invalid task status %q", t.Status)
	}
	data, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("store: marshal task: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(BucketTasks))
		if err != nil {
			return err
		}
		return b.Put([]byte(t.ID), data)
	})
}

// GetTask returns the Task stored under id. The error wraps
// fs.ErrNotExist when the bucket is missing or the key is absent so
// callers can tell "no such task" apart from a transport error.
func (s *Store) GetTask(id string) (Task, error) {
	var (
		out  Task
		data []byte
	)
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketTasks))
		if b == nil {
			return fmt.Errorf("store: get task %q: %w", id, fs.ErrNotExist)
		}
		v := b.Get([]byte(id))
		if v == nil {
			return fmt.Errorf("store: get task %q: %w", id, fs.ErrNotExist)
		}
		// Copy out of the bolt-managed slice before the View
		// transaction returns.
		data = append(data[:0], v...)
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return Task{}, fmt.Errorf("store: decode task %q: %w", id, err)
	}
	return out, nil
}

// DeleteTask removes the JSON-encoded Task stored under id from
// BucketTasks. The error wraps fs.ErrNotExist when the bucket is
// missing or the key is absent so callers (notably `j tasks discard`)
// can distinguish "no such task" from a transport error and surface
// the correct user-facing message. Bolt-level failures (closed DB,
// disk error) propagate verbatim from db.Update; PutTask follows
// the same convention so the surfacing is uniform.
func (s *Store) DeleteTask(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketTasks))
		if b == nil {
			return fmt.Errorf("store: delete task %q: %w", id, fs.ErrNotExist)
		}
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("store: delete task %q: %w", id, fs.ErrNotExist)
		}
		return b.Delete([]byte(id))
	})
}

// ListTasks returns every task stored in BucketTasks. A missing bucket
// yields an empty slice and a nil error so callers can treat
// "no tasks yet" identically to "no bucket yet". Decoding errors are
// surfaced wrapped: a corrupted entry is a real bug, not an empty list.
func (s *Store) ListTasks() ([]Task, error) {
	var out []Task
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketTasks))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var t Task
			if err := json.Unmarshal(v, &t); err != nil {
				return fmt.Errorf("store: decode task %q: %w", k, err)
			}
			out = append(out, t)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SortTasks orders tasks the way `j tasks` displays them:
//
//  1. Active states (planning, working, verifying, help) come first,
//     among themselves sorted by ID ascending. ID is time-sortable so
//     this is effectively "earliest started first" with a stable
//     tiebreak.
//  2. Inactive states (planned, done, plus any future non-active
//     status) come after, sorted by done_at descending. When done_at
//     is missing we fall back to work_end_at, then plan_end_at, then
//     finally to ID descending so newer-started entries float up.
//
// The function mutates tasks in place and returns nothing because the
// receiver convention here matches sort.Slice's existing call sites.
func SortTasks(tasks []Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		ai, aj := taskIsActive(tasks[i].Status), taskIsActive(tasks[j].Status)
		if ai != aj {
			return ai
		}
		if ai {
			return tasks[i].ID < tasks[j].ID
		}
		ti, tj := taskFallbackTime(tasks[i]), taskFallbackTime(tasks[j])
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return tasks[i].ID > tasks[j].ID
	})
}

// taskIsActive returns true for the four "still in flight" statuses.
// Anything else (plan-done, work-done, verify-done, completed, plus
// any future inactive state) is treated as inactive by SortTasks.
func taskIsActive(s TaskStatus) bool {
	switch s {
	case StatusPlanning, StatusWorking, StatusVerifying, StatusHelp:
		return true
	}
	return false
}

// taskFallbackTime returns the timestamp SortTasks compares for
// inactive tasks. The cascade reflects how a task's lifecycle ends:
// completed tasks have done_at; verify-done tasks only have
// verify_end_at; work-done tasks only have work_end_at; plan-done
// tasks only have plan_end_at; anything older falls through to the
// zero time so ID order takes over.
func taskFallbackTime(t Task) time.Time {
	switch {
	case t.DoneAt != nil:
		return *t.DoneAt
	case t.VerifyEndAt != nil:
		return *t.VerifyEndAt
	case t.WorkEndAt != nil:
		return *t.WorkEndAt
	case t.PlanEndAt != nil:
		return *t.PlanEndAt
	}
	return time.Time{}
}

// SummarizeMarkdown derives Task.Summary from a markdown body: the
// first non-empty line, with leading "#" / space markers stripped and
// the result truncated to summaryMaxRunes runes. Empty input yields
// an empty summary so callers can decide whether to substitute a
// placeholder.
func SummarizeMarkdown(body string) string {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		line = strings.TrimLeft(line, "# ")
		return truncateRunes(line, summaryMaxRunes)
	}
	return ""
}

// truncateRunes returns s if it is at most max runes long, otherwise
// the first max runes. Operating on runes (not bytes) keeps multibyte
// UTF-8 input from being cut mid-codepoint.
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// worktreeSlugMaxRunes bounds each slug segment (project and task)
// in WorktreeNameFor. The cap keeps the resulting worktree name
// short enough to be a usable directory / branch component without
// getting in the way of `git worktree list` output.
const worktreeSlugMaxRunes = 48

// WorktreeNameFor returns the deterministic, human-readable worktree
// name for t inside project. The result is `<project-slug>-<task-slug>`,
// where each component is slugify'd (lowercase, non-[a-z0-9] runs
// collapsed to single dashes, edges trimmed, clipped to
// worktreeSlugMaxRunes runes). The task slug is derived from
// t.Summary when non-empty and falls back to the lowercased
// t.ID (a 26-char Crockford base32 ULID) so pre-summary rows still
// produce a valid name. An empty project slug yields just the task
// slug so tests that run outside a recognisable project directory
// still get a meaningful value.
//
// Examples:
//
//   - project "j", summary "Drop the legacy tasks file"
//     -> "j-drop-the-legacy-tasks-file"
//   - project "j", empty summary, id "01KQ..."
//     -> "j-01kq..."
func WorktreeNameFor(project string, t Task) string {
	projectSlug := slugify(project, worktreeSlugMaxRunes)
	taskSlug := slugify(t.Summary, worktreeSlugMaxRunes)
	if taskSlug == "" {
		// Ulid ids are always alphanumeric so slugify is effectively
		// a lowercase here; running them through slugify anyway means
		// an unexpected non-alphanumeric rune in t.ID still produces
		// a clean slug rather than leaking raw punctuation.
		taskSlug = slugify(t.ID, worktreeSlugMaxRunes)
	}
	if projectSlug == "" {
		return taskSlug
	}
	if taskSlug == "" {
		return projectSlug
	}
	return projectSlug + "-" + taskSlug
}

// slugify lowercases s, replaces every run of non-[a-z0-9] runes with
// a single `-`, trims leading/trailing `-`, and clips the result to
// max runes. An empty or pure-separator input yields "" so callers
// can fall back to a secondary id.
func slugify(s string, max int) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	prevDash := true
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	return truncateRunes(out, max)
}
// PersistWarn opens `<cwd>/.j/tasks/list.db`, PutTask's the row, and
// closes the store. Path-resolve, open, and put failures each surface
// as a single `warning: tasks ...` line on stderr and the helper
// returns; persistence is best-effort by design so the phase
// lifecycle keeps running even when the row cannot be written.
// Designed to be called twice per phase run — once at begin, once at
// finish — so the bbolt file lock is never held across the agent
// invocation in between. Mirrors the inline open/close convention in
// PersistAgentSelection.
func PersistWarn(stderr io.Writer, task Task) {
	path, err := DefaultTasksDBPath()
	if err != nil {
		fmt.Fprintf(stderr, "warning: tasks path: %v\n", err)
		return
	}
	s, err := Open(path)
	if err != nil {
		fmt.Fprintf(stderr, "warning: tasks db: %v\n", err)
		return
	}
	defer func() { _ = s.Close() }()
	if err := s.PutTask(task); err != nil {
		fmt.Fprintf(stderr, "warning: tasks put: %v\n", err)
	}
}

// ReadRequirementSidecar derives the path to the original requirement
// markdown from a plan path produced by `j plan`'s legacy
// `<dir>/<stem>.plan.md` convention and returns its contents when
// readable. When the plan path does not follow this convention, or
// the sidecar file does not exist / cannot be read, an empty string
// is returned so the caller falls back to the plan body for the
// summary.
func ReadRequirementSidecar(planPath string) string {
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

// Summary picks a one-line summary in this order:
//  1. first non-empty line of the requirement / plan markdown,
//  2. the requirement file basename when the body was unreadable.
//
// Truncation is delegated to SummarizeMarkdown for the body path; the
// basename path is short by construction. Shared by the plan and
// work phases (work wraps it via FromPlanAndRequirement to add the
// plan-body fallback).
func Summary(requirement, target string) string {
	if s := SummarizeMarkdown(requirement); s != "" {
		return s
	}
	if target != "" {
		return filepath.Base(target)
	}
	return ""
}

// PickSource returns whichever of the refined requirements or the
// plan body has a usable first non-empty line, preferring the
// requirements summary because that is the document the agent
// rewrote to capture user intent. Both empty falls through to the
// file basename in Summary.
func PickSource(refinedRequirements, planMarkdown string) string {
	if SummarizeMarkdown(refinedRequirements) != "" {
		return refinedRequirements
	}
	return planMarkdown
}

// FromPlanAndRequirement mirrors Summary's precedence for `j work`:
// requirement first, plan body second, file basename last. Kept
// separate from Summary so the plan flow (which only has one body
// candidate at begin time) does not need to pass an empty plan body
// just to reuse the work-flow fallback chain.
func FromPlanAndRequirement(requirement, planBody, planPath string) string {
	if s := SummarizeMarkdown(requirement); s != "" {
		return s
	}
	if s := SummarizeMarkdown(planBody); s != "" {
		return s
	}
	if planPath != "" {
		return filepath.Base(planPath)
	}
	return ""
}

// PlanLifecycle owns the begin/end task-log writes around a single
// agent.Plan invocation. The struct holds no bbolt handle — every
// task-log write goes through PersistWarn, which opens, writes, and
// closes within the same call so the bbolt file lock is never held
// across agent.Plan and a concurrent `j tasks` from another shell is
// not blocked. The lifecycle is constructed with NewPlanTask /
// Task.BeginPlanReuse and finalised with Finish; callers pair them
// with a defer so the task is always written even when agent.Plan
// panics.
type PlanLifecycle struct {
	stderr io.Writer
	task   Task
	closed bool
}

// NewPlanTask records the "planning" entry for a real plan run. The
// caller passes the freshly-minted task id (so the per-task directory
// under <cwd>/.j/tasks/ uses the same id as the bbolt row), the
// markdown target the user is planning against (used for the basename
// fallback when the body has no usable first line), the requirement
// body, and the plan-phase resume token (empty for agents with no
// notion of resume or on a NewResumeID failure already warned by the
// caller).
//
// Best effort: failure to open the task log or to write the initial
// row warns once on stderr and execution continues.
func NewPlanTask(stderr io.Writer, agentName, model, taskID, target, requirement, resumeID string) *PlanLifecycle {
	begin := time.Now().UTC()
	task := Task{
		ID:               taskID,
		Status:           StatusPlanning,
		InvokedTool:      agentName,
		InvokedModel:     model,
		PlanResumeCursor: resumeID,
		Summary:          Summary(requirement, target),
		PlanBeginAt:      &begin,
	}
	lc := &PlanLifecycle{stderr: stderr, task: task}
	PersistWarn(stderr, task)
	return lc
}

// BeginPlanReuse mutates a copy of the receiver to flip status to
// `planning` for the re-plan flow. PlanEndAt and DoneAt are cleared
// so the finalize step stamps fresh values; the original
// PlanBeginAt is preserved verbatim when set so the row keeps its
// first-run lineage. Tool/model and the plan resume cursor are
// refreshed so the row reflects the latest re-plan invocation.
//
// The body / source-path are intentionally not touched: re-plan
// reads requirements.md from the existing task directory and feeds
// it back through agent.Plan, so the summary derivation runs again
// in Finish.
func (t Task) BeginPlanReuse(stderr io.Writer, agentName, model, resumeID string) *PlanLifecycle {
	begin := time.Now().UTC()
	task := t
	task.Status = StatusPlanning
	task.InvokedTool = agentName
	task.InvokedModel = model
	task.PlanResumeCursor = resumeID
	task.PlanEndAt = nil
	task.DoneAt = nil
	if task.PlanBeginAt == nil {
		task.PlanBeginAt = &begin
	}
	lc := &PlanLifecycle{stderr: stderr, task: task}
	PersistWarn(stderr, task)
	return lc
}

// RecordBackground stamps the spawned child's PID and the agent log
// path on the in-memory task row and re-persists it. It is the
// counterpart of Finish for fire-and-forget headless runs: the row
// stays at status `planning` until the reaper in `j tasks` observes
// the child exited and finalises it.
//
// RecordBackground sets the closed flag so a defensive Finish fired
// by mistake (e.g. via a deferred guard) becomes a silent no-op and
// does not clobber the background row with `plan-done` / `help`.
func (lc *PlanLifecycle) RecordBackground(pid int, logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.BackgroundPID = pid
	lc.task.AgentLogPath = logPath
	PersistWarn(lc.stderr, lc.task)
}

// Finish stamps plan_end_at, decides the terminal status from runErr,
// and (when runErr is nil) re-derives Summary from the refined
// requirements (then the plan body, then the file basename) because
// the agent may have rewritten the requirements during the session.
// The task is rewritten to the log even on errors so `help` is
// observable from `j tasks`. The bbolt store is opened just long
// enough to write the row and closed before this returns; calling
// Finish twice is a silent no-op via the closed flag.
func (lc *PlanLifecycle) Finish(runErr error, refinedRequirements, planMarkdown, target string) {
	if lc.closed {
		return
	}
	lc.closed = true
	end := time.Now().UTC()
	lc.task.PlanEndAt = &end
	if runErr != nil {
		lc.task.Status = StatusHelp
	} else {
		lc.task.Status = StatusPlanDone
		lc.task.Summary = Summary(PickSource(refinedRequirements, planMarkdown), target)
	}
	PersistWarn(lc.stderr, lc.task)
}

// Task returns the in-memory snapshot of the task row. The plan flow
// uses this for symmetry with WorkLifecycle.Task; the field is a
// value copy so callers cannot mutate the lifecycle's internal state.
func (lc *PlanLifecycle) Task() Task { return lc.task }

// WorkLifecycle owns the begin/end task-log writes around a single
// agent.Work invocation. Mirrors PlanLifecycle: the struct holds no
// bbolt handle — every task-log write goes through PersistWarn so
// the bbolt file lock is never held across agent.Work and a
// concurrent `j tasks` from another shell is not blocked.
//
// Constructed with NewWorkTask, Task.BeginWorkReuse, or
// Task.BeginWorkResume depending on whether the run is a legacy file
// import (creates a new bbolt row), a bbolt-sourced run (mutates an
// existing row in place), or a resume.
type WorkLifecycle struct {
	stderr io.Writer
	task   Task
	closed bool
}

// NewWorkTask records the "working" entry for a legacy `--from-file`
// import. The caller has already minted the task id and staged the
// plan markdown into <cwd>/.j/tasks/<id>/plan.md (and optionally
// requirements.md). This helper just stamps the bbolt row.
//
// Worktree is minted via WorktreeNameFor so the worker and the
// verifier share one rule; callers that pre-populate Worktree (none
// today — `j plan` does not set it) still have their value preserved.
func NewWorkTask(stderr io.Writer, agentName, model, taskID, planPath, requirement, planBody, resumeID string) *WorkLifecycle {
	begin := time.Now().UTC()
	task := Task{
		ID:               taskID,
		Status:           StatusWorking,
		InvokedTool:      agentName,
		InvokedModel:     model,
		WorkResumeCursor: resumeID,
		Summary:          FromPlanAndRequirement(requirement, planBody, planPath),
		WorkBeginAt:      &begin,
	}
	fillWorktree(&task)
	return openWorkLifecycle(stderr, task)
}

// BeginWorkReuse mutates a copy of the receiver to flip status to
// `working`, stamp work_begin_at, clear stale work_end_at / done_at
// from a previous failed run, and record the latest tool/model and
// resume cursor for the work phase. Plan-phase fields are preserved.
//
// A pre-existing Worktree on the receiver is kept verbatim (so manual
// edits persist); an empty one is populated via WorktreeNameFor so
// rows that pre-date the field still gain a meaningful name on their
// first bbolt-sourced `j work`.
func (t Task) BeginWorkReuse(stderr io.Writer, agentName, model, resumeID string) *WorkLifecycle {
	begin := time.Now().UTC()
	task := t
	task.Status = StatusWorking
	task.InvokedTool = agentName
	task.InvokedModel = model
	task.WorkResumeCursor = resumeID
	task.WorkBeginAt = &begin
	task.WorkEndAt = nil
	task.DoneAt = nil
	fillWorktree(&task)
	return openWorkLifecycle(stderr, task)
}

// BeginWorkResume is the resume-flow companion of BeginWorkReuse. The
// two functions diverge in two places:
//
//  1. The existing WorkResumeCursor is preserved verbatim instead of
//     being overwritten with a fresh `Agent.NewResumeID` value (the
//     whole point of resume is reusing the cursor recorded on the
//     task row).
//  2. The original WorkBeginAt timestamp is preserved when set so the
//     task row keeps its first-run lineage; only WorkEndAt / DoneAt
//     are cleared so Finish stamps fresh values on the next finalize.
//     Tool/model are kept verbatim because resume never re-prompts
//     the user for them.
func (t Task) BeginWorkResume(stderr io.Writer) *WorkLifecycle {
	task := t
	task.Status = StatusWorking
	task.WorkEndAt = nil
	task.DoneAt = nil
	if task.WorkBeginAt == nil {
		begin := time.Now().UTC()
		task.WorkBeginAt = &begin
	}
	return openWorkLifecycle(stderr, task)
}

// openWorkLifecycle is the shared helper that best-effort writes the
// initial row and returns a WorkLifecycle suitable for Finish.
func openWorkLifecycle(stderr io.Writer, task Task) *WorkLifecycle {
	lc := &WorkLifecycle{stderr: stderr, task: task}
	PersistWarn(stderr, task)
	return lc
}

// fillWorktree populates task.Worktree via WorktreeNameFor when it is
// empty, leaving a pre-existing value untouched. A ProjectName lookup
// failure (cwd removed while the process runs) is treated as "no
// project slug" so the helper still mints a task-only slug instead
// of bailing: `j work` has more important things to do than surface
// a hard error for a cosmetic worktree label.
func fillWorktree(task *Task) {
	if task.Worktree != "" {
		return
	}
	project, _ := ProjectName()
	task.Worktree = WorktreeNameFor(project, *task)
}

// RecordBackground stamps the spawned child's PID and the agent log
// path on the in-memory work task row and re-persists it. The row
// stays at status `working` until the reaper in `j tasks` observes
// the child exited and finalises it.
//
// RecordBackground sets the closed flag so a defensive Finish fired
// by mistake becomes a silent no-op and does not clobber the
// background row with `work-done` / `help`.
func (lc *WorkLifecycle) RecordBackground(pid int, logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.BackgroundPID = pid
	lc.task.AgentLogPath = logPath
	PersistWarn(lc.stderr, lc.task)
}

// Finish stamps work_end_at, picks the terminal status from runErr
// (work-done on success, help on error), and rewrites the task. The
// `completed` status (and DoneAt) is reserved for `j verify`; `j work`
// no longer terminates the lifecycle here. Calling Finish twice is a
// silent no-op via the closed flag.
func (lc *WorkLifecycle) Finish(runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	end := time.Now().UTC()
	lc.task.WorkEndAt = &end
	if runErr != nil {
		lc.task.Status = StatusHelp
	} else {
		lc.task.Status = StatusWorkDone
	}
	PersistWarn(lc.stderr, lc.task)
}

// Task returns the in-memory snapshot of the work task row. Used by
// `j work` to read the freshly-minted Worktree value for the
// agent.Work request without poking at the unexported struct field.
func (lc *WorkLifecycle) Task() Task { return lc.task }

// VerifyOutcome enumerates the terminal results of `j verify`'s
// fix-loop. VerifyOutcomeSuccess means the verifier returned VERDICT:
// PASS at some iteration; the task can be finalised as `completed`.
// VerifyOutcomeNoRetries means the loop exhausted MaxIterations
// without a PASS verdict; the task ends as `verify-done`. Errors are
// surfaced separately via the runErr argument so VerifyLifecycle.Finish
// can pick the `help` status.
type VerifyOutcome int

const (
	// VerifyOutcomeSuccess: verifier returned PASS; finalise as
	// `completed` with DoneAt stamped.
	VerifyOutcomeSuccess VerifyOutcome = iota
	// VerifyOutcomeNoRetries: loop exhausted without a PASS; finalise
	// as `verify-done`.
	VerifyOutcomeNoRetries
)

// VerifyLifecycle owns the begin/end task-log writes around a single
// `j verify` invocation. Mirrors WorkLifecycle: the struct holds no
// bbolt handle — every task-log write goes through PersistWarn so
// the bbolt file lock is never held across agent.Verify and a
// concurrent `j tasks` from another shell is not blocked.
type VerifyLifecycle struct {
	stderr io.Writer
	task   Task
	closed bool
}

// BeginVerify flips an existing task row to `verifying`, stamps
// VerifyBeginAt, clears stale VerifyEndAt / DoneAt from a previous
// failed run, and records the latest tool/model and resume cursor
// for the verify phase. Plan-phase and work-phase fields are
// preserved.
func (t Task) BeginVerify(stderr io.Writer, agentName, model, resumeID string) *VerifyLifecycle {
	begin := time.Now().UTC()
	task := t
	task.Status = StatusVerifying
	task.InvokedTool = agentName
	task.InvokedModel = model
	task.VerifyResumeCursor = resumeID
	task.VerifyBeginAt = &begin
	task.VerifyEndAt = nil
	task.DoneAt = nil
	return openVerifyLifecycle(stderr, task)
}

// BeginVerifyResume is the resume-flow companion of BeginVerify. It
// diverges from BeginVerify in two places:
//
//  1. The existing VerifyResumeCursor is preserved verbatim instead
//     of being overwritten with a fresh `Agent.NewResumeID` value.
//  2. The original VerifyBeginAt timestamp is preserved when set so
//     the task row keeps its first-run lineage; only VerifyEndAt /
//     DoneAt are cleared so Finish stamps fresh values on the next
//     finalize. Tool/model are kept verbatim because resume never
//     re-prompts the user for them.
func (t Task) BeginVerifyResume(stderr io.Writer) *VerifyLifecycle {
	task := t
	task.Status = StatusVerifying
	task.VerifyEndAt = nil
	task.DoneAt = nil
	if task.VerifyBeginAt == nil {
		begin := time.Now().UTC()
		task.VerifyBeginAt = &begin
	}
	return openVerifyLifecycle(stderr, task)
}

// openVerifyLifecycle is the shared helper that best-effort writes
// the initial row and returns a VerifyLifecycle suitable for Finish.
func openVerifyLifecycle(stderr io.Writer, task Task) *VerifyLifecycle {
	lc := &VerifyLifecycle{stderr: stderr, task: task}
	PersistWarn(stderr, task)
	return lc
}

// RecordBackground stamps the spawned child's PID and the agent log
// path on the in-memory verify task row and re-persists it. The row
// stays at status `verifying` until the reaper in `j tasks` observes
// the child exited and finalises it.
func (lc *VerifyLifecycle) RecordBackground(pid int, logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.BackgroundPID = pid
	lc.task.AgentLogPath = logPath
	PersistWarn(lc.stderr, lc.task)
}

// Finish stamps verify_end_at, picks the terminal status from
// (outcome, runErr), and rewrites the task row.
//
//   - runErr != nil → status `help`, DoneAt unchanged.
//   - outcome == VerifyOutcomeSuccess → status `completed`, DoneAt
//     stamped.
//   - outcome == VerifyOutcomeNoRetries → status `verify-done`,
//     DoneAt unchanged.
//
// Calling Finish twice is a silent no-op via the closed flag.
func (lc *VerifyLifecycle) Finish(outcome VerifyOutcome, runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	end := time.Now().UTC()
	lc.task.VerifyEndAt = &end
	switch {
	case runErr != nil:
		lc.task.Status = StatusHelp
	case outcome == VerifyOutcomeSuccess:
		lc.task.Status = StatusCompleted
		done := time.Now().UTC()
		lc.task.DoneAt = &done
	default:
		lc.task.Status = StatusVerifyDone
	}
	PersistWarn(lc.stderr, lc.task)
}
