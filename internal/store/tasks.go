package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	bolt "go.etcd.io/bbolt"
)

// BucketTasks is the bucket inside the per-project task log DB
// (`<cwd>/.j/tasks`) that holds JSON-encoded Task values keyed by
// Task.ID. It is shared by the writers in `j plan` / `j work` and the
// reader in `j tasks`.
const BucketTasks = "tasks"

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
	Summary      string     `json:"summary"`

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
}

// taskCounter is a process-local monotonically-increasing counter used
// by NewTaskID to disambiguate IDs minted within the same nanosecond.
// It is safe for concurrent use.
var taskCounter atomic.Uint64

// NewTaskID returns a stable, unique, lexicographically time-sortable
// task identifier. The leading 16 hex digits are
// time.Now().UTC().UnixNano(); the trailing 8 hex digits come from a
// process-local atomic counter so two IDs minted in the same
// nanosecond still differ. The `-` separator keeps the two halves
// human-readable in `j tasks` output.
func NewTaskID() string {
	n := time.Now().UTC().UnixNano()
	c := taskCounter.Add(1)
	return fmt.Sprintf("%016x-%08x", uint64(n), c)
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
