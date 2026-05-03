// Package work implements the `j work` subcommand. It resolves a plan
// to execute (by --from-task <id>, --from-file, the most recent plan-done
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

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/cli/agentpick"
	"github.com/spacelions/j/internal/cli/tasklog"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/mustread"
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
	// Yes, when true, skips the status-mismatch confirmation prompt
	// and proceeds even when the resolved task is not in the
	// plan-done / help allowlist. Mirrors the `--yes` / WORK_YES
	// flag wiring on the cobra command.
	Yes bool

	// Interactive is a tri-state: a non-nil value is the explicit
	// user choice (cobra `--interactive` flag or WORK_INTERACTIVE
	// env var), and nil means "not set, fall back to the stored
	// `interactive` value or the cobra default true". Stored wins
	// when Interactive is nil and the bucket has a parseable value;
	// explicit always wins.
	Interactive *bool

	// Tool and Model are one-off overrides for the worker bucket's
	// recorded tool/model. When either is set, Run resolves the
	// worker via agentpick.Resolve (filling the missing half from
	// the bucket if needed) and skips persistence: the bucket is
	// left untouched. When both are empty, Run falls back to the
	// existing read-then-prompt-then-persist precedence.
	Tool  string
	Model string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI

	// Store, when non-nil, receives best-effort writes recording the
	// tool/model/interactive flag last used. The work source is
	// intentionally NOT persisted: the user must supply or be
	// prompted for it every run. The orchestrator does not own the
	// lifecycle when the caller supplies a Store. When nil, the
	// helpers below open `<cwd>/.j/settings` only for the duration
	// of each individual read/write so the bbolt file lock is never
	// held across the long-running agent.Work invocation. Tests
	// that supply a Store directly skip the open/close cycle
	// entirely.
	Store *store.Store
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
//
// User-abort signals from any huh prompt (Ctrl+C / Esc) propagate up
// as huh.ErrUserAborted; the deferred guard below converts them to a
// nil return so an explicit cancel exits the command cleanly without
// printing a bogus "cancelled by user" line. Genuine errors keep
// their original wrapping.
//
// The bbolt file lock on `<cwd>/.j/settings` is never held across the
// agent.Work call: each settings read/write below opens the DB,
// performs the operation, and closes before any agent work begins so
// concurrent `j settings` / `j tasks` invocations are not blocked on
// the OS file lock.
func Run(ctx context.Context, opts Options) (err error) {
	defer func() {
		if errors.Is(err, huh.ErrUserAborted) {
			err = nil
		}
	}()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("J: no coding agents configured")
	}
	// Resolve the effective interactive flag once so the same
	// value flows into both the agent request and the work-done
	// row (and into persistWorkerSelection on the prompted path).
	// Precedence: explicit (opts.Interactive != nil) > stored
	// (bucket has parseable value) > cobra default true.
	opts.Interactive = boolPtr(resolveInteractive(opts))

	res, ok, err := resolvePlan(ctx, opts)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	agent, model, err := selectWorker(ctx, opts)
	if err != nil {
		return err
	}

	resumeID, err := agent.NewResumeID(ctx)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "warning: %v\n", err)
	}

	var lc *workLifecycle
	if res.Existing != nil {
		proceed, confirmErr := confirmStatusOverride(ctx, opts, "work", *res.Existing, allowedForWork)
		if confirmErr != nil {
			return confirmErr
		}
		if !proceed {
			return nil
		}
		lc = beginWorkTaskReuse(opts, agent, model, *res.Existing, resumeID)
	} else {
		lc = beginWorkTaskNew(opts, agent, model, res.NewTaskID, res.PlanPath, res.Requirement, res.Body, resumeID)
	}

	taskID := workTaskID(res)
	agentLogPath := filepath.Join(filepath.Dir(res.PlanPath), tasklog.AgentLogFileName)
	mustreadFiles, mustreadErr := mustread.LoadFromDefault()
	if mustreadErr != nil {
		fmt.Fprintf(opts.Stderr, "warning: %v\n", mustreadErr)
	}
	pid, workErr := agent.Work(ctx, codingagents.WorkRequest{
		PlanPath:     res.PlanPath,
		Body:         res.Body,
		Model:        model,
		Interactive:  *opts.Interactive,
		ResumeChatID: resumeID,
		Worktree:     lc.task.Worktree,
		AgentLogPath: agentLogPath,
		Mustread:     mustreadFiles,
	})
	if workErr == nil && pid > 0 {
		lc.recordBackground(pid, agentLogPath)
		fmt.Fprintf(opts.Stdout,
			"J: %s running in background (PID=%d); see .j/tasks/%s/%s\n",
			agent.Name(), pid, taskID, tasklog.AgentLogFileName)
		return nil
	}
	lc.finishWork(workErr)
	if workErr != nil {
		return workErr
	}

	if res.Existing != nil {
		fmt.Fprintf(opts.Stdout, "J: coding on task %s\n", res.Existing.ID)
	} else {
		fmt.Fprintf(opts.Stdout, "J: coding on %s\n", res.PlanPath)
	}
	return nil
}

// workTaskID returns the task id (existing or newly minted) that the
// work flow is operating against. Both the bbolt-sourced reuse path
// and the legacy file-import path map to a single per-task directory
// keyed by id; the helper picks whichever the caller has populated.
func workTaskID(res resolved) string {
	if res.Existing != nil {
		return res.Existing.ID
	}
	return res.NewTaskID
}

// resolvePlan implements the precedence: --from-task > --from-file (legacy
// import) > pick latest plan-done auto-pick > UI picker over every task.
// Each branch returns a fully-populated resolved or a wrapped error;
// callers do not need to re-stat or re-read files afterwards.
//
// When the bbolt store contains exactly one row whose status is in
// the natural work allowlist (plan-done / help), the no-flag path
// auto-picks it without prompting — this preserves the
// historic happy-path UX for the common case of a fresh `j plan`
// followed by `j work`. Otherwise the picker shows every task and
// the downstream confirm prompt handles the wrong-status case.
//
// The bool return is the "proceed" signal from the unified
// taskpick contract: ok=false means the user cancelled the picker
// (Ctrl-C / Esc) and the caller should exit cleanly without
// invoking the agent. ok=true means the resolved struct is
// populated and Run can continue.
func resolvePlan(ctx context.Context, opts Options) (resolved, bool, error) {
	switch {
	case opts.TaskID != "":
		r, err := resolveByTaskID(opts, opts.TaskID)
		return r, err == nil, err
	case opts.FromFile != "":
		r, err := resolveFromFile(opts, opts.FromFile)
		return r, err == nil, err
	}
	tasks, err := listResolvableWorkTasks(opts)
	if err != nil {
		return resolved{}, false, err
	}
	if len(tasks) == 0 {
		raw, err := opts.UI.AskFromFile(ctx)
		if err != nil {
			return resolved{}, false, err
		}
		r, err := resolveFromFile(opts, raw)
		return r, err == nil, err
	}
	if id, ok := autoPickAllowed(tasks, allowedForWork); ok {
		r, err := resolveByTaskID(opts, id)
		return r, err == nil, err
	}
	chosen, ok, err := opts.UI.PickPlanTask(ctx, tasks)
	if err != nil {
		return resolved{}, false, err
	}
	if !ok {
		return resolved{}, false, nil
	}
	r, err := resolveByTaskID(opts, chosen)
	return r, err == nil, err
}

// autoPickAllowed returns the single task id and ok=true when
// exactly one of the supplied tasks is in the allowlist (i.e. the
// happy-path auto-pick from the historic listPlanDoneTasks
// behaviour). Any other count surfaces ok=false so the caller
// renders the picker over the full slice.
func autoPickAllowed(tasks []store.Task, allowed func(store.Task) bool) (string, bool) {
	var picked string
	count := 0
	for _, t := range tasks {
		if allowed(t) {
			picked = t.ID
			count++
		}
	}
	if count == 1 {
		return picked, true
	}
	return "", false
}

// resolveByTaskID loads an existing task row, then reads
// .j/tasks/<id>/plan.md and (best-effort) requirements.md. The id is
// trusted (it came from a previous EnsureTaskDir call that staged
// the row) so we derive paths via filepath.Join instead of round-
// tripping through the path helpers and their Getwd error returns.
func resolveByTaskID(opts Options, id string) (resolved, error) {
	s, ok := tasklog.OpenTaskLog(opts.Stderr)
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
	requirement := tasklog.ReadRequirementSidecar(src)

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

// listResolvableWorkTasks returns every task in bbolt sorted via
// store.SortTasks. The picker now surfaces every row regardless of
// status; the downstream confirm prompt handles the wrong-status
// case (per the re-plan / re-work contract). The happy-path
// auto-pick still kicks in via autoPickAllowed when exactly one of
// the rows is in the work allowlist.
func listResolvableWorkTasks(opts Options) ([]store.Task, error) {
	s, ok := tasklog.OpenTaskLog(opts.Stderr)
	if !ok {
		return nil, errors.New("work: tasks db unavailable")
	}
	defer func() { _ = s.Close() }()
	all, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	store.SortTasks(all)
	return all, nil
}

// allowedForWork is the natural status allowlist for `j work`:
// plan-done (the happy path after `j plan`) and help (retry after
// a failed run). Anything else is allowed by `j work` only after
// the user confirms the prompt (or via --yes / WORK_YES); this
// preserves the prior UX for the common case while letting users
// re-run work against in-flight or post-work tasks.
func allowedForWork(t store.Task) bool {
	switch t.Status {
	case store.StatusPlanDone, store.StatusHelp:
		return true
	}
	return false
}

// confirmStatusOverride decides whether to run agent.Work against a
// task whose status falls outside the allowlist. Allowlist match
// → proceed silently. Otherwise --yes / WORK_YES → proceed
// silently; else delegate to the UI confirm prompt and return its
// bool. A user decline (false from the prompt) returns
// proceed=false with err=nil so the caller can exit cleanly.
func confirmStatusOverride(ctx context.Context, opts Options, cmd string, t store.Task, allowed func(store.Task) bool) (bool, error) {
	if allowed(t) {
		return true, nil
	}
	if opts.Yes {
		return true, nil
	}
	return opts.UI.ConfirmStatusOverride(ctx, cmd, t.ID, string(t.Status))
}

// selectWorker is the single chokepoint for choosing the worker
// tool/model. Precedence:
//  1. explicit --tool / --model (opts.Tool or opts.Model set) →
//     agentpick.Resolve fills the missing half from the worker
//     bucket and runs CheckLogin. The bucket is NOT written.
//  2. populated worker bucket → agentpick.FromStore reuses it.
//  3. otherwise → agentpick.Pick prompts the user and the result is
//     persisted to the worker bucket.
//
// Settings DB access is short-lived: the bucket is read inside
// workerFromStore and the handle is released before this returns so
// the agent.Work call downstream never contends on the bbolt file
// lock.
func selectWorker(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	if opts.Tool != "" || opts.Model != "" {
		agent, model, err := workerResolveExplicit(ctx, opts)
		if err == nil {
			return agent, model, nil
		}
		if !errors.Is(err, agentpick.ErrNoStoredSelection) {
			return nil, "", err
		}
	}
	agent, model, err := workerFromStore(ctx, opts)
	if err == nil {
		return agent, model, nil
	}
	if !errors.Is(err, agentpick.ErrNoStoredSelection) {
		return nil, "", err
	}
	fmt.Fprintln(opts.Stderr, "Choose your favourite:")
	agent, model, err = agentpick.Pick(ctx, opts.UI, opts.Agents)
	if err != nil {
		return nil, "", err
	}
	persistWorkerSelection(opts, agent.Name(), model)
	return agent, model, nil
}

// workerResolveExplicit reads the worker bucket only to fill the
// missing half of the user-supplied --tool / --model pair. When
// opts.Store is non-nil it is reused; otherwise this opens
// `<cwd>/.j/settings` for the duration of the read and releases it
// before returning so the file lock is not held across agent.Work.
func workerResolveExplicit(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	if opts.Store != nil {
		return agentpick.Resolve(ctx, opts.Store, store.BucketWorker, opts.Agents, opts.Tool, opts.Model)
	}
	s, ok := openSettingsStore(opts.Stderr)
	if !ok {
		return agentpick.Resolve(ctx, nil, store.BucketWorker, opts.Agents, opts.Tool, opts.Model)
	}
	defer func() { _ = s.Close() }()
	return agentpick.Resolve(ctx, s, store.BucketWorker, opts.Agents, opts.Tool, opts.Model)
}

// workerFromStore reads the worker bucket and returns the chosen
// tool/model. When opts.Store is non-nil (test injection) it is reused
// without any open/close cycle. Otherwise this opens
// `<cwd>/.j/settings` only for the duration of agentpick.FromStore and
// releases it before returning. A failure to open the settings DB
// surfaces as ErrNoStoredSelection so the caller falls back to the
// prompt path the same way an empty bucket would.
func workerFromStore(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	if opts.Store != nil {
		return agentpick.FromStore(ctx, opts.Store, store.BucketWorker, opts.Agents)
	}
	s, ok := openSettingsStore(opts.Stderr)
	if !ok {
		return nil, "", agentpick.ErrNoStoredSelection
	}
	defer func() { _ = s.Close() }()
	return agentpick.FromStore(ctx, s, store.BucketWorker, opts.Agents)
}

// persistWorkerSelection writes the just-confirmed tool/model and the
// interactive flag to the worker bucket. The plan path (the work
// "source") is intentionally NOT persisted so the user picks one per
// run. Persistence is best-effort: errors warn on opts.Stderr and
// don't abort the run. When opts.Store is non-nil it is used directly
// (test injection); otherwise this opens `<cwd>/.j/settings` for the
// duration of the write and closes it immediately so the file lock
// is not held across the agent invocation.
func persistWorkerSelection(opts Options, tool, model string) {
	// opts.Interactive is normally non-nil here: Run resolves it
	// via resolveInteractive before any selection branch fires.
	// The nil-guard below keeps the helper callable from tests
	// that construct a bare Options{} for the nil-store smoke
	// path; resolveInteractive's default of true is reproduced
	// verbatim.
	interactive := true
	if opts.Interactive != nil {
		interactive = *opts.Interactive
	}
	if opts.Store != nil {
		store.PersistAgentSelection(opts.Store, opts.Stderr, store.BucketWorker, tool, model, interactive)
		return
	}
	s, ok := openSettingsStore(opts.Stderr)
	if !ok {
		return
	}
	defer func() { _ = s.Close() }()
	store.PersistAgentSelection(s, opts.Stderr, store.BucketWorker, tool, model, interactive)
}

// resolveInteractive applies the documented precedence (explicit >
// stored > cobra default true) and returns a concrete bool. Pulled
// out of Run to keep the early-setup block readable and testable in
// isolation.
func resolveInteractive(opts Options) bool {
	if opts.Interactive != nil {
		return *opts.Interactive
	}
	if v, ok := storedWorkerInteractive(opts); ok {
		return v
	}
	return true
}

// storedWorkerInteractive looks up the worker bucket's `interactive`
// entry. When opts.Store is non-nil it is reused; otherwise the
// settings DB is opened and closed solely for this read so the lock
// is not held across the agent call. A failed open or a missing /
// unparseable value yields (_, false) so callers fall back to the
// cobra default.
func storedWorkerInteractive(opts Options) (bool, bool) {
	if opts.Store != nil {
		return agentpick.StoredInteractive(opts.Store, store.BucketWorker)
	}
	s, ok := openSettingsStore(opts.Stderr)
	if !ok {
		return false, false
	}
	defer func() { _ = s.Close() }()
	return agentpick.StoredInteractive(s, store.BucketWorker)
}

// boolPtr is the package-private companion that lets Run / tests
// build a non-nil *bool from a literal without spelling out a temp
// variable at every call site.
func boolPtr(b bool) *bool { return &b }

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
	return o
}

// openSettingsStore opens `<cwd>/.j/settings` for the worker. It is
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
