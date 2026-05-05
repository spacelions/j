package tasks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// AgentLogFileName is the per-task file that captures stdout/stderr
// of a fire-and-forget headless coding-agent child. It lives at
// `<cwd>/.j/tasks/<id>/agent.log` and is written to each task row's
// AgentLogPath so `j tasks` and the user can find it later. All
// phases (plan / work / verify) share this filename so the reaper in
// `j tasks` can surface the log regardless of which command spawned
// the child.
const AgentLogFileName = "agent.log"

// TaskFileName is the per-task TOML file that holds the row metadata
// (status, summary, resume cursors, phase timestamps, background PID).
// It lives alongside requirements.md / plan.md / agent.log inside
// `<cwd>/.j/tasks/<id>/`. One file per task means concurrent writers
// to different tasks never contend, and atomic write+rename guarantees
// readers see either the old row or the new one — never partial.
const TaskFileName = "task.toml"

// VerifierPlanFileName is the filename of the verifier's draft
// verification plan stored under `<cwd>/.j/tasks/<id>/`. Written by
// `j verify` (via the agent's tool calls) and read by `j tasks`
// summary derivation.
const VerifierPlanFileName = "verifier_plan.md"

// VerifierFindingsFileName is the filename of the verifier's findings
// markdown stored under `<cwd>/.j/tasks/<id>/`. Its last non-empty
// line is parsed by the orchestrator into a PASS/FAIL verdict.
const VerifierFindingsFileName = "verifier_findings.md"

// Task is the value persisted to `<cwd>/.j/tasks/<id>/task.toml`.
// Pointer-typed time fields are omitted when unknown so a partially-
// completed task (planning in flight, work not started) never claims
// fake timestamps. JSON tags are preserved for the agent log markers
// emitted via internal/util/agentlog; the on-disk TOML format is
// produced via the taskWire shadow type below (see the comment on
// taskWire for the pelletier `*time.Time` workaround).
//
// The body markdown is canonical on disk too:
// `<cwd>/.j/tasks/<id>/requirements.md` and
// `<cwd>/.j/tasks/<id>/plan.md`. task.toml carries metadata only.
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
	// then preserved on every subsequent transition. Empty for tasks
	// worked before the field was introduced; downstream agents
	// treat empty as "fall back to the main checkout".
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

// taskWire is the on-disk projection of Task for TOML serialisation.
// pelletier/go-toml/v2 has a known asymmetry where `*time.Time`
// encodes as a quoted TOML string (literal) instead of a TOML
// datetime, and the resulting file does not round-trip. Using
// `time.Time` (value) sidesteps that bug, at the cost of always
// rendering the field even when unset; we use the zero value
// (`0001-01-01T00:00:00Z`) as the "not set" sentinel and translate
// to/from `*time.Time` at the encode/decode boundary so the in-memory
// Task API stays unchanged.
type taskWire struct {
	ID                 string     `toml:"id"`
	Status             TaskStatus `toml:"status"`
	InvokedTool        string     `toml:"invoked_tool"`
	InvokedModel       string     `toml:"invoked_model"`
	Worktree           string     `toml:"worktree"`
	Summary            string     `toml:"summary"`
	PlanResumeCursor   string     `toml:"plan_resume_cursor"`
	WorkResumeCursor   string     `toml:"work_resume_cursor"`
	VerifyResumeCursor string     `toml:"verify_resume_cursor"`
	PlanBeginAt        time.Time  `toml:"plan_begin_at"`
	PlanEndAt          time.Time  `toml:"plan_end_at"`
	WorkBeginAt        time.Time  `toml:"work_begin_at"`
	WorkEndAt          time.Time  `toml:"work_end_at"`
	VerifyBeginAt      time.Time  `toml:"verify_begin_at"`
	VerifyEndAt        time.Time  `toml:"verify_end_at"`
	DoneAt             time.Time  `toml:"done_at"`
	BackgroundPID      int        `toml:"background_pid"`
	AgentLogPath       string     `toml:"agent_log_path"`
}

// derefTime returns *p, or the zero time when p is nil.
func derefTime(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return *p
}

// optTimePtr returns a pointer to a copy of v, or nil when v is the
// zero time (our "not set" sentinel).
func optTimePtr(v time.Time) *time.Time {
	if v.IsZero() {
		return nil
	}
	cp := v
	return &cp
}

func taskToWire(t Task) taskWire {
	return taskWire{
		ID: t.ID, Status: t.Status,
		InvokedTool: t.InvokedTool, InvokedModel: t.InvokedModel,
		Worktree: t.Worktree, Summary: t.Summary,
		PlanResumeCursor:   t.PlanResumeCursor,
		WorkResumeCursor:   t.WorkResumeCursor,
		VerifyResumeCursor: t.VerifyResumeCursor,
		PlanBeginAt:        derefTime(t.PlanBeginAt),
		PlanEndAt:          derefTime(t.PlanEndAt),
		WorkBeginAt:        derefTime(t.WorkBeginAt),
		WorkEndAt:          derefTime(t.WorkEndAt),
		VerifyBeginAt:      derefTime(t.VerifyBeginAt),
		VerifyEndAt:        derefTime(t.VerifyEndAt),
		DoneAt:             derefTime(t.DoneAt),
		BackgroundPID:      t.BackgroundPID,
		AgentLogPath:       t.AgentLogPath,
	}
}

func wireToTask(w taskWire) Task {
	return Task{
		ID: w.ID, Status: w.Status,
		InvokedTool: w.InvokedTool, InvokedModel: w.InvokedModel,
		Worktree: w.Worktree, Summary: w.Summary,
		PlanResumeCursor:   w.PlanResumeCursor,
		WorkResumeCursor:   w.WorkResumeCursor,
		VerifyResumeCursor: w.VerifyResumeCursor,
		PlanBeginAt:        optTimePtr(w.PlanBeginAt),
		PlanEndAt:          optTimePtr(w.PlanEndAt),
		WorkBeginAt:        optTimePtr(w.WorkBeginAt),
		WorkEndAt:          optTimePtr(w.WorkEndAt),
		VerifyBeginAt:      optTimePtr(w.VerifyBeginAt),
		VerifyEndAt:        optTimePtr(w.VerifyEndAt),
		DoneAt:             optTimePtr(w.DoneAt),
		BackgroundPID:      w.BackgroundPID,
		AgentLogPath:       w.AgentLogPath,
	}
}

// PutTask TOML-encodes t and writes it to
// `<tasksDir>/<t.ID>/task.toml` via atomic write+rename. The per-task
// directory is created if missing so callers do not need to invoke
// EnsureTaskDir first. The status allowlist is enforced here so a
// misspelled enum surfaces as a deterministic error instead of
// corrupting the listing logic downstream. Concurrent writers to
// different task IDs never contend (separate files); concurrent
// writers to the same ID are last-writer-wins via os.Rename, which
// matches the previous bbolt semantics.
func (s *Store) PutTask(t Task) error {
	if t.ID == "" {
		return errors.New("store: task id required")
	}
	if !t.Status.Valid() {
		return fmt.Errorf("store: invalid task status %q", t.Status)
	}
	if s.tasksDir == "" {
		return errors.New("store: PutTask called on non-tasks store")
	}
	taskDir := filepath.Join(s.tasksDir, t.ID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir %q: %w", taskDir, err)
	}
	data, err := toml.Marshal(taskToWire(t))
	if err != nil {
		return fmt.Errorf("store: marshal task: %w", err)
	}
	return writeFileAtomic(filepath.Join(taskDir, TaskFileName), data, 0o644)
}

// GetTask returns the Task stored under id. The error wraps
// fs.ErrNotExist when no `task.toml` exists for the id so callers can
// tell "no such task" apart from a transport error.
func (s *Store) GetTask(id string) (Task, error) {
	if s.tasksDir == "" {
		return Task{}, errors.New("store: GetTask called on non-tasks store")
	}
	path := filepath.Join(s.tasksDir, id, TaskFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Task{}, fmt.Errorf("store: get task %q: %w", id, fs.ErrNotExist)
		}
		return Task{}, fmt.Errorf("store: read task %q: %w", id, err)
	}
	var w taskWire
	if err := toml.Unmarshal(data, &w); err != nil {
		return Task{}, fmt.Errorf("store: decode task %q: %w", id, err)
	}
	return wireToTask(w), nil
}

// DeleteTask removes the per-task `task.toml` for id. The error wraps
// fs.ErrNotExist when no row exists for the id so callers (notably
// `j tasks discard`) can distinguish "no such task" from a transport
// error and surface the correct user-facing message. Other failures
// (read-only fs, etc.) propagate wrapped. The per-task directory and
// its other artifacts (requirements.md, plan.md, agent.log) are
// preserved; `j tasks discard` removes them via RemoveTaskDir.
func (s *Store) DeleteTask(id string) error {
	if s.tasksDir == "" {
		return errors.New("store: DeleteTask called on non-tasks store")
	}
	path := filepath.Join(s.tasksDir, id, TaskFileName)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("store: delete task %q: %w", id, fs.ErrNotExist)
		}
		return fmt.Errorf("store: stat task %q: %w", id, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("store: remove task %q: %w", id, err)
	}
	return nil
}

// ListTasks returns every task by walking `<tasksDir>/<id>/task.toml`.
// A missing tasks directory yields an empty slice and a nil error so
// callers can treat "no tasks yet" identically to "no project yet".
// Subdirectories without a task.toml (e.g. mid-creation) are silently
// skipped. Decoding errors are surfaced wrapped: a corrupted entry is
// a real bug, not an empty list. Results are sorted by ID (ULIDs are
// time-sortable) so listing order is deterministic.
func (s *Store) ListTasks() ([]Task, error) {
	if s.tasksDir == "" {
		return nil, errors.New("store: ListTasks called on non-tasks store")
	}
	entries, err := os.ReadDir(s.tasksDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: readdir %q: %w", s.tasksDir, err)
	}
	out := make([]Task, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(s.tasksDir, entry.Name(), TaskFileName)
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("store: read %q: %w", path, err)
		}
		var w taskWire
		if err := toml.Unmarshal(data, &w); err != nil {
			return nil, fmt.Errorf("store: decode task %q: %w", entry.Name(), err)
		}
		out = append(out, wireToTask(w))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
