package tasks

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
// (status, summary, resume sessions, phase timestamps, agent log path).
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
// Value time.Time fields use the zero value as "not set"; callers use
// .IsZero() to check whether a phase timestamp was recorded.
// pelletier/go-toml/v2 v2.3.0 has two known bugs: (1) *time.Time
// encodes as a quoted TOML string instead of a datetime, and (2)
// time.Time with omitempty is always suppressed even for non-zero
// values. Both bugs are documented in wire_test.go. The workaround is
// value time.Time without omitempty; zero timestamps write as
// 0001-01-01T00:00:00Z and are skipped by .IsZero() on read.
//
// The body markdown is canonical on disk too:
// `<cwd>/.j/tasks/<id>/requirements.md` and
// `<cwd>/.j/tasks/<id>/plan.md`. task.toml carries metadata only.
//
// Each phase mints its own resume token via Agent.NewResumeID; an
// empty string means "no session for that phase yet".
type Task struct {
	ID     string     `toml:"id"`
	Status TaskStatus `toml:"status"`

	PlanTool    string `toml:"plan_tool,omitempty"`
	PlanModel   string `toml:"plan_model,omitempty"`
	WorkTool    string `toml:"work_tool,omitempty"`
	WorkModel   string `toml:"work_model,omitempty"`
	VerifyTool  string `toml:"verify_tool,omitempty"`
	VerifyModel string `toml:"verify_model,omitempty"`
	// Worktree is the bare git-worktree name (no slashes, no path)
	// the worker and verifier should operate against for this task.
	// It is minted by `j work` on first run via WorktreeNameFor and
	// then preserved on every subsequent transition. Empty for tasks
	// worked before the field was introduced; downstream agents
	// treat empty as "fall back to the main checkout".
	Worktree string `toml:"worktree,omitempty"`
	Summary  string `toml:"summary"`

	PlanResumeSession   string `toml:"plan_resume_session,omitempty"`
	WorkResumeSession   string `toml:"work_resume_session,omitempty"`
	VerifyResumeSession string `toml:"verify_resume_session,omitempty"`

	PlanBeginAt   time.Time `toml:"plan_begin_at"`
	PlanEndAt     time.Time `toml:"plan_end_at"`
	WorkBeginAt   time.Time `toml:"work_begin_at"`
	WorkEndAt     time.Time `toml:"work_end_at"`
	VerifyBeginAt time.Time `toml:"verify_begin_at"`
	VerifyEndAt   time.Time `toml:"verify_end_at"`
	DoneAt        time.Time `toml:"done_at"`

	// AgentLogPath is the absolute path of the per-task log file that
	// captures the spawned child's stdout/stderr (typically
	// `<cwd>/.j/tasks/<id>/agent.log`). It is set whenever a
	// background spawn was attempted so users can follow a backgrounded
	// run; the reaper does not clear it after the row is finalised so
	// the trailing log remains discoverable.
	AgentLogPath string `toml:"agent_log_path,omitempty"`

	// LinearIssue is the upstream `<TEAM>-<NUM>` identifier when the
	// task was created from a Linear issue (via `j plan --from-linear`,
	// `j tasks start --from-linear`, or the source picker's Linear
	// branch). Empty for markdown / re-plan sources. The value is
	// preserved across re-plans so `j tasks` can keep surfacing the
	// original Linear link.
	LinearIssue string `toml:"linear_issue,omitempty"`

	// PullRequestURL is the GitHub PR URL the worker agent produced
	// (detected from agent.log or `gh pr list --head`). Empty until
	// the tuiquit reconciler fills it in on TUI exit or the reaper
	// populates it. Upstream display surfaces it as a link in the
	// task table.
	PullRequestURL string `toml:"pull_request_url,omitempty"`

	// ProcessedPRCommands stores stable GitHub command comment ids
	// already handled by the manual PR-feedback command path.
	ProcessedPRCommands []string `toml:"processed_pr_commands,omitempty"`
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
	data, err := toml.Marshal(t)
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
	var t Task
	if err := toml.Unmarshal(data, &t); err != nil {
		return Task{}, fmt.Errorf("store: decode task %q: %w", id, err)
	}
	return t, nil
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
		var t Task
		if err := toml.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("store: decode task %q: %w", entry.Name(), err)
		}
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b Task) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

// DisplayToolModel returns the tool and model to surface in `j tasks` for this
// row. The pair is chosen by status so each phase's values are preserved
// independently on disk.
//
// For StatusHelp and StatusNeedsClarification the deepest phase that
// has a non-empty tool is used so the listing shows where the task
// was when it got stuck.
func (t Task) DisplayToolModel() (tool, model string) {
	switch t.Status {
	case StatusPlanning, StatusPlanPendingApproval, StatusPlanDone:
		return t.PlanTool, t.PlanModel
	case StatusWorking, StatusWorkDone:
		return t.WorkTool, t.WorkModel
	case StatusVerifying, StatusFailed, StatusCompleted:
		return t.VerifyTool, t.VerifyModel
	case StatusHelp, StatusNeedsClarification:
		if t.VerifyTool != "" {
			return t.VerifyTool, t.VerifyModel
		}
		if t.WorkTool != "" {
			return t.WorkTool, t.WorkModel
		}
		return t.PlanTool, t.PlanModel
	}
	return t.PlanTool, t.PlanModel
}
