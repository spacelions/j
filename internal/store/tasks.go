package store

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	bolt "go.etcd.io/bbolt"
)

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
// missing or the key is absent so callers (notably `j tasks delete`)
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
