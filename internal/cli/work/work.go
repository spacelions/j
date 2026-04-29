// Package work implements the `j work` subcommand. It resolves a plan
// to execute (by --task <id>, --from-file, the most recent plan-done
// task in bbolt, or an interactive picker), prompts the user for a
// coding agent and model, verifies that backend is signed in, and
// hands the plan to the agent so it can edit files in place. The
// orchestrator does not write any output file.
package work

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/agentpick"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/util/mdfile"
)

// Options configures Run. Stdin/Stdout/Stderr default to the process
// streams. UI defaults to the huh implementation. Agents must be
// supplied by the caller (the CLI wires the Cursor agent; tests inject
// scripted ones). Interactive selects the agent's TUI when true and
// the headless path when false.
type Options struct {
	// TaskID, when set, names an existing task whose plan.md should be
	// executed. The task row is updated in place (plan-done -> working
	// -> work-done|help). Beats FromFile when both are supplied.
	TaskID string
	// FromFile is a legacy escape hatch: the path of a plan markdown
	// file outside .j/tasks/. The file is imported into a fresh
	// .j/tasks/<new-id>/plan.md folder and a NEW task row is created.
	FromFile string

	Interactive bool

	// FromSettings, when true, makes Run reuse the tool/model
	// recorded in the coder bucket of <cwd>/.j/settings instead of
	// prompting. When the bucket is empty (first run) Run falls back
	// to the interactive Pick flow and emits a single stderr warning.
	// This field is session-only and is intentionally NOT persisted
	// to the bbolt store; the cobra layer supplies the default
	// (true) so the zero value here is fine.
	FromSettings bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI

	// Store, when non-nil, receives best-effort writes recording the
	// tool/model/interactive flag last used. The work source is
	// intentionally NOT persisted: the user must supply or be
	// prompted for it every run. The orchestrator does not own the
	// lifecycle when the caller supplies a Store. When nil,
	// withDefaults opens the default <cwd>/.j/settings DB and closes
	// it after Run returns.
	Store *store.Store

	// closeStore is set internally by withDefaults when it allocates
	// the default Store, so Run can close it before returning. Tests
	// that pass their own Store leave this false.
	closeStore bool
}

// resolved is the outcome of resolvePlan: it tells Run which path to
// take for task logging and what to hand to the agent.
type resolved struct {
	// Existing is the bbolt task row to mutate when non-nil. When nil
	// the run is a legacy file import and a NEW task row is created.
	Existing *store.Task
	// PlanPath is the absolute path of the plan markdown to execute.
	// For Existing!=nil it is <cwd>/.j/tasks/<id>/plan.md; for legacy
	// imports it is <cwd>/.j/tasks/<new-id>/plan.md after the import.
	PlanPath string
	// Body is the plan markdown contents.
	Body string
	// Requirement is the requirement markdown body when available
	// (read from <cwd>/.j/tasks/<id>/requirements.md or, for legacy
	// imports, the `<stem>.md` sidecar of FromFile).
	Requirement string
	// NewTaskID, set only on the legacy file-import path, is the id
	// of the freshly created task folder.
	NewTaskID string
}

// Run executes `j work`. It resolves the plan source (Options.TaskID,
// Options.FromFile, latest plan-done bbolt row, then UI picker), then
// dispatches to the reuse or import path.
func Run(ctx context.Context, opts Options) error {
	opts = opts.withDefaults()
	if opts.closeStore && opts.Store != nil {
		defer func() { _ = opts.Store.Close() }()
	}
	if len(opts.Agents) == 0 {
		return errors.New("work: no coding agents configured")
	}

	res, err := resolvePlan(ctx, opts)
	if err != nil {
		return err
	}

	agent, model, err := selectCoder(ctx, opts)
	if err != nil {
		return err
	}

	resumeID, err := agent.NewResumeID(ctx)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "warning: %v\n", err)
	}

	var lc *workLifecycle
	if res.Existing != nil {
		if err := validateForWork(*res.Existing); err != nil {
			return err
		}
		lc = beginWorkTaskReuse(opts, agent, model, *res.Existing, resumeID)
	} else {
		lc = beginWorkTaskNew(opts, agent, model, res.NewTaskID, res.PlanPath, res.Requirement, res.Body, resumeID)
	}

	workErr := agent.Work(ctx, codingagents.WorkRequest{
		PlanPath:     res.PlanPath,
		Body:         res.Body,
		Model:        model,
		Interactive:  opts.Interactive,
		ResumeChatID: resumeID,
	})
	lc.finishWork(workErr)
	if workErr != nil {
		return workErr
	}

	if res.Existing != nil {
		fmt.Fprintf(opts.Stdout, "coding against task %s\n", res.Existing.ID)
	} else {
		fmt.Fprintf(opts.Stdout, "coding against %s\n", res.PlanPath)
	}
	return nil
}

// resolvePlan implements the precedence: --task > --from-file (legacy
// import) > pick latest plan-done > UI picker. Each branch returns a
// fully-populated resolved or a wrapped error; callers do not need to
// re-stat or re-read files afterwards.
func resolvePlan(ctx context.Context, opts Options) (resolved, error) {
	switch {
	case opts.TaskID != "":
		return resolveByTaskID(opts, opts.TaskID)
	case opts.FromFile != "":
		return resolveFromFile(opts, opts.FromFile)
	}
	tasks, err := listPlanDoneTasks(opts)
	if err != nil {
		return resolved{}, err
	}
	switch len(tasks) {
	case 0:
		raw, err := opts.UI.AskFromFile(ctx)
		if err != nil {
			return resolved{}, err
		}
		return resolveFromFile(opts, raw)
	case 1:
		return resolveByTaskID(opts, tasks[0].ID)
	}
	chosen, err := opts.UI.PickPlanTask(ctx, tasks)
	if err != nil {
		return resolved{}, err
	}
	return resolveByTaskID(opts, chosen)
}

// resolveByTaskID loads an existing task row, then reads
// .j/tasks/<id>/plan.md and (best-effort) requirements.md. The id is
// trusted (it came from a previous EnsureTaskDir call that staged
// the row) so we derive paths via filepath.Join instead of round-
// tripping through the path helpers and their Getwd error returns.
func resolveByTaskID(opts Options, id string) (resolved, error) {
	s, ok := openTaskLog(opts.Stderr)
	if !ok {
		return resolved{}, errors.New("work: tasks db unavailable")
	}
	defer func() { _ = s.Close() }()
	task, err := s.GetTask(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return resolved{}, fmt.Errorf("work: task %q not found", id)
		}
		return resolved{}, err
	}
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		return resolved{}, err
	}
	taskDir := filepath.Join(tasksDir, id)
	planPath := filepath.Join(taskDir, store.PlanFileName)
	body, err := os.ReadFile(planPath)
	if err != nil {
		return resolved{}, fmt.Errorf("work: read plan: %w", err)
	}
	var requirement string
	if data, readErr := os.ReadFile(filepath.Join(taskDir, store.RequirementsFileName)); readErr == nil {
		requirement = string(data)
	}
	return resolved{
		Existing:    &task,
		PlanPath:    planPath,
		Body:        string(body),
		Requirement: requirement,
	}, nil
}

// resolveFromFile imports an external plan markdown into a fresh
// .j/tasks/<new-id>/ directory so legacy file users still produce a
// task folder. The requirement markdown is recovered from the
// `<stem>.md` sidecar when present, mirroring the previous convention.
func resolveFromFile(opts Options, raw string) (resolved, error) {
	src, err := mdfile.Resolve(raw)
	if err != nil {
		return resolved{}, err
	}
	body, err := os.ReadFile(src)
	if err != nil {
		return resolved{}, fmt.Errorf("work: read plan: %w", err)
	}
	requirement := readRequirementSidecar(src)

	id := store.NewTaskID()
	dir, err := store.EnsureTaskDir(id)
	if err != nil {
		return resolved{}, fmt.Errorf("work: ensure task dir: %w", err)
	}
	planPath := filepath.Join(dir, store.PlanFileName)
	if err := os.WriteFile(planPath, body, 0o644); err != nil {
		return resolved{}, fmt.Errorf("work: write plan: %w", err)
	}
	if requirement != "" {
		reqPath := filepath.Join(dir, store.RequirementsFileName)
		if err := os.WriteFile(reqPath, []byte(requirement), 0o644); err != nil {
			fmt.Fprintf(opts.Stderr, "warning: write requirements: %v\n", err)
		}
	}
	return resolved{
		PlanPath:    planPath,
		Body:        string(body),
		Requirement: requirement,
		NewTaskID:   id,
	}, nil
}

// listPlanDoneTasks returns all plan-done tasks in bbolt sorted with
// the most recent first (SortTasks already groups active first; we
// filter here so the UI picker only shows actionable rows).
func listPlanDoneTasks(opts Options) ([]store.Task, error) {
	s, ok := openTaskLog(opts.Stderr)
	if !ok {
		return nil, errors.New("work: tasks db unavailable")
	}
	defer func() { _ = s.Close() }()
	all, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	store.SortTasks(all)
	out := all[:0]
	for _, t := range all {
		if t.Status == store.StatusPlanDone {
			out = append(out, t)
		}
	}
	return out, nil
}

// validateForWork rejects starting `j work` against a task whose
// status would clobber unrelated state. Allowed entry statuses are
// plan-done (the happy path) and help (retry after a failed run).
// Anything else is an error so users do not accidentally re-run work
// against a task that is currently in flight or already past the
// work phase.
func validateForWork(t store.Task) error {
	switch t.Status {
	case store.StatusPlanDone, store.StatusHelp:
		return nil
	case store.StatusPlanning:
		return fmt.Errorf("work: task %s is still planning", t.ID)
	case store.StatusWorking:
		return fmt.Errorf("work: task %s is already working", t.ID)
	case store.StatusWorkDone, store.StatusVerifying, store.StatusVerifyDone, store.StatusCompleted:
		return fmt.Errorf("work: task %s already past work phase (status %s)", t.ID, t.Status)
	}
	return fmt.Errorf("work: task %s has unsupported status %q", t.ID, t.Status)
}

// selectCoder is the single chokepoint for choosing the coder
// tool/model. When FromSettings is true it tries the read-only
// agentpick.FromStore path first and only falls back to the
// interactive Pick flow on ErrNoStoredSelection (printing a single
// stderr line so the user knows why they're being prompted). The
// just-confirmed selection is persisted only on the prompted path:
// values that came from the store are already there.
func selectCoder(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	if opts.FromSettings {
		agent, model, err := agentpick.FromStore(ctx, opts.Store, store.BucketCoder, opts.Agents)
		if err == nil {
			return agent, model, nil
		}
		if !errors.Is(err, agentpick.ErrNoStoredSelection) {
			return nil, "", err
		}
		fmt.Fprintln(opts.Stderr, "no stored coder selection; prompting")
	}
	agent, model, err := agentpick.Pick(ctx, opts.UI, opts.Agents)
	if err != nil {
		return nil, "", err
	}
	persistCoderSelection(opts, agent.Name(), model)
	return agent, model, nil
}

// persistCoderSelection writes the just-confirmed tool/model and the
// interactive flag to the coder bucket. The plan path (the work
// "source") is intentionally NOT persisted so the user picks one per
// run. Persistence is best-effort: errors warn on opts.Stderr and
// don't abort the run.
func persistCoderSelection(opts Options, tool, model string) {
	store.PersistAgentSelection(opts.Store, opts.Stderr, store.BucketCoder, tool, model, opts.Interactive)
}

func (o Options) withDefaults() Options {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.UI == nil {
		o.UI = newHuhUI(o.Stdin, o.Stderr)
	}
	if o.Store == nil {
		if s, ok := openSettingsStore(o.Stderr); ok {
			o.Store = s
			o.closeStore = true
		}
	}
	return o
}

// openSettingsStore opens `<cwd>/.j/settings` for the coder. It is
// the post-init replacement for store.OpenDefault: pre-flight has
// already created the layout, so failures here are real (e.g.
// concurrent locks) and surface as a single "warning: ..." line on
// stderr.
func openSettingsStore(stderr io.Writer) (*store.Store, bool) {
	path, err := store.DefaultPath()
	if err != nil {
		fmt.Fprintf(stderr, "warning: settings path: %v\n", err)
		return nil, false
	}
	s, err := store.Open(path)
	if err != nil {
		fmt.Fprintf(stderr, "warning: settings db: %v\n", err)
		return nil, false
	}
	return s, true
}

// openTaskLog opens `<cwd>/.j/tasks/list.db` for the work flow. Like
// openSettingsStore this is the post-init replacement for
// store.OpenTaskLog: pre-flight ensures the file exists, so any
// failure here is reported once on stderr and the lifecycle
// degrades to a nil-store no-op.
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
