// Package plan implements the `j plan` subcommand. It collects a planning
// source (markdown today, linear later), asks for a model and a coding
// agent backend, verifies that backend is signed in, and runs it to
// produce a refined requirements summary and a plan stored under
// <cwd>/.j/tasks/<id>/. No file is written to the workspace; `j tasks`
// lists the runs and `j work --from-task <id>` executes the plan.
package plan

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
// scripted ones). Interactive selects the agent's TUI when true and the
// headless capture path when false.
type Options struct {
	// FromFile is the markdown task description path (from --from-file
	// or PLAN_FROM_FILE). When empty the orchestrator prompts via the
	// UI.
	FromFile string
	// TaskID, when set, names an existing task whose
	// `<cwd>/.j/tasks/<id>/requirements.md` should be re-planned
	// in place. The task row is updated in place (status →
	// planning → plan-done|help, original PlanBeginAt
	// preserved). Beats FromFile when both are supplied.
	TaskID string
	// Yes, when true, skips the status-mismatch confirmation
	// prompt and proceeds even when the resolved task is not in
	// the plan-done / help allowlist. Mirrors the `--yes` /
	// PLAN_YES flag wiring on the cobra command.
	Yes bool
	// Interactive is a tri-state: a non-nil value is the explicit
	// user choice (cobra `--interactive` flag or PLAN_INTERACTIVE
	// env var), and nil means "not set, fall back to the stored
	// `interactive` value or the cobra default true". Stored wins
	// when Interactive is nil and the bucket has a parseable value;
	// explicit always wins.
	Interactive *bool

	// Tool and Model are one-off overrides for the planner bucket's
	// recorded tool/model. When either is set, Run resolves the
	// planner via agentpick.Resolve (filling the missing half from
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
	// tool/model/interactive flag last used (the plan source and the
	// markdown source path are intentionally NOT persisted). The
	// orchestrator does not own the lifecycle: callers that supply a
	// Store keep the lifecycle. When nil, the helpers below open
	// `<cwd>/.j/settings` only for the duration of each individual
	// read/write so the bbolt file lock is never held across the
	// long-running agent invocation. Tests that supply a Store
	// directly skip the open/close cycle entirely.
	Store *store.Store
}

// Run executes `j plan`. When Options.FromFile is set it goes straight
// to the markdown source (preserving --from-file/PLAN_FROM_FILE
// semantics). Otherwise it asks the user which source to use and
// dispatches.
//
// User-abort signals from any huh prompt (Ctrl+C / Esc) propagate up
// as huh.ErrUserAborted; the deferred guard below converts them to a
// nil return so an explicit cancel exits the command cleanly without
// printing a bogus "cancelled by user" line. Genuine errors keep
// their original wrapping.
//
// The bbolt file lock on `<cwd>/.j/settings` is never held across the
// agent.Plan call: each settings read/write below opens the DB,
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
		return errors.New("plan: no coding agents configured")
	}
	// Resolve the effective interactive flag once so the same
	// value flows into both the agent request and the plan-done
	// row (and into persistPlannerSelection on the prompted path).
	// Precedence: explicit (opts.Interactive != nil) > stored
	// (bucket has parseable value) > cobra default true.
	opts.Interactive = boolPtr(resolveInteractive(opts))

	// --from-task short-circuits the source picker; the explicit
	// flag is unambiguous so we head straight to the re-plan
	// flow without prompting for a source.
	if opts.TaskID != "" {
		return runReplanTask(ctx, opts, opts.TaskID)
	}
	if opts.FromFile != "" {
		return runMarkdown(ctx, opts, opts.FromFile)
	}

	src, err := opts.UI.SelectSource(ctx)
	if err != nil {
		return err
	}
	switch src {
	case SourceMarkdown:
		target, err := pickMarkdownTarget(ctx, opts)
		if err != nil {
			return err
		}
		return runMarkdown(ctx, opts, target)
	case SourceLinear:
		fmt.Fprintln(opts.Stdout, "plan: linear source is not yet wired up; nothing to do")
		return nil
	case SourceTask:
		id, err := pickReplanTarget(ctx, opts)
		if err != nil {
			return err
		}
		if id == "" {
			return nil
		}
		return runReplanTask(ctx, opts, id)
	}
	return fmt.Errorf("plan: unsupported source %s", src)
}

// pickReplanTarget lists every task in bbolt (sorted via
// store.SortTasks) and asks the user to pick one for the re-plan
// flow. An empty list surfaces a clean error mentioning the cwd
// so users see why nothing is available; otherwise the chosen id
// is returned for runReplanTask. The picker now uses the unified
// (id, ok, err) contract from internal/cli/taskpick: ok=false
// (user aborted Ctrl-C / Esc) collapses to ("", nil) so Run can
// short-circuit cleanly without relying on the deferred
// huh.ErrUserAborted guard for this hop. Genuine UI errors
// propagate verbatim.
func pickReplanTarget(ctx context.Context, opts Options) (string, error) {
	tasks, err := listAllTasks(opts)
	if err != nil {
		return "", err
	}
	if len(tasks) == 0 {
		return "", errors.New("plan: no tasks to re-plan; run `j plan` first")
	}
	id, ok, err := opts.UI.PickReplanTask(ctx, tasks)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	return id, nil
}

// runReplanTask is the re-plan flow: load the existing task row,
// confirm status if necessary, read the existing requirements.md,
// pick a tool/model, and re-run agent.Plan against the same task
// directory so requirements.md and plan.md are refreshed in
// place. The task row is mutated (status: planning → plan-done,
// preserving original PlanBeginAt).
func runReplanTask(ctx context.Context, opts Options, id string) error {
	existing, err := loadTaskByID(opts, id)
	if err != nil {
		return err
	}
	proceed, err := confirmStatusOverride(ctx, opts, "re-plan", existing, allowedForReplan)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		return err
	}
	taskDir := filepath.Join(tasksDir, existing.ID)
	requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
	planPath := filepath.Join(taskDir, store.PlanFileName)

	agent, model, err := selectPlanner(ctx, opts)
	if err != nil {
		return err
	}

	resumeID, err := agent.NewResumeID(ctx)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "warning: %v\n", err)
	}
	agentLogPath := filepath.Join(taskDir, tasklog.AgentLogFileName)
	mustReadFiles, mustReadErr := mustread.LoadFromDefault()
	if mustReadErr != nil {
		fmt.Fprintf(opts.Stderr, "warning: %v\n", mustReadErr)
	}
	lc := beginPlanTaskReuse(opts, agent, model, existing, resumeID)
	pid, planErr := agent.Plan(ctx, codingagents.PlanRequest{
		FromFilePath:           requirementsPath,
		Model:                  model,
		RequirementsOutputPath: requirementsPath,
		PlanOutputPath:         planPath,
		Interactive:            *opts.Interactive,
		ResumeChatID:           resumeID,
		AgentLogPath:           agentLogPath,
		MustRead:               mustReadFiles,
	})

	if planErr == nil && pid > 0 {
		lc.recordBackground(pid, agentLogPath)
		fmt.Fprintf(opts.Stdout,
			"J: cursor-agent running in background (PID=%d); see .j/tasks/%s/%s\n",
			pid, existing.ID, tasklog.AgentLogFileName)
		return nil
	}

	var refinedReq, planMD string
	if planErr == nil {
		if data, readErr := os.ReadFile(requirementsPath); readErr == nil {
			refinedReq = string(data)
		} else {
			fmt.Fprintf(opts.Stderr, "warning: read %s: %v\n", requirementsPath, readErr)
		}
		if data, readErr := os.ReadFile(planPath); readErr == nil {
			planMD = string(data)
		} else {
			fmt.Fprintf(opts.Stderr, "warning: read %s: %v\n", planPath, readErr)
		}
	}
	lc.finishPlan(planErr, refinedReq, planMD, requirementsPath)
	if planErr != nil {
		return planErr
	}

	fmt.Fprintf(opts.Stdout, "J: re-planned task %s\n", existing.ID)
	return nil
}

// loadTaskByID opens the tasks DB, reads the row, and closes the
// handle before returning. fs.ErrNotExist is rewrapped into the
// user-facing "task %q not found" so the caller does not need to
// translate it.
func loadTaskByID(opts Options, id string) (store.Task, error) {
	s, ok := tasklog.OpenTaskLog(opts.Stderr)
	if !ok {
		return store.Task{}, errors.New("plan: tasks db unavailable")
	}
	defer func() { _ = s.Close() }()
	task, err := s.GetTask(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return store.Task{}, fmt.Errorf("plan: task %q not found", id)
		}
		return store.Task{}, err
	}
	return task, nil
}

// listAllTasks returns every task in bbolt, sorted via
// store.SortTasks so the picker shows the active-then-most-recent
// order users see in `j tasks`. The store is closed before the
// slice is returned so the agent invocation downstream does not
// contend on the bbolt file lock.
func listAllTasks(opts Options) ([]store.Task, error) {
	s, ok := tasklog.OpenTaskLog(opts.Stderr)
	if !ok {
		return nil, errors.New("plan: tasks db unavailable")
	}
	defer func() { _ = s.Close() }()
	all, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	store.SortTasks(all)
	return all, nil
}

// allowedForReplan is the natural status allowlist for the re-plan
// flow: plan-done (user is iterating on the plan after a previous
// plan run) and help (retry after a failed planning run). Tasks
// in any other status trigger the confirm prompt unless --yes /
// PLAN_YES skips it.
func allowedForReplan(t store.Task) bool {
	switch t.Status {
	case store.StatusPlanDone, store.StatusHelp:
		return true
	}
	return false
}

// confirmStatusOverride decides whether to run agent.Plan against a
// task whose status falls outside the allowlist. The allowlist
// returns true → proceed silently. Otherwise --yes / PLAN_YES →
// proceed silently; else delegate to the UI confirm prompt and
// return its bool. A user decline (false from the prompt) returns
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

// pickMarkdownTarget scans the current working directory for markdown
// files and asks the UI to choose one. Replaces the legacy free-text
// prompt: the user can no longer mistype a path, and AGENTS.md /
// README.md / hidden files / non-`.md` files never appear. An empty
// scan surfaces a clean error mentioning the cwd so the agent is
// never invoked. The chosen basename is mapped back to the matching
// absolute path before being handed to runMarkdown so downstream
// behaviour (mdfile.Resolve, agent.Plan) is identical to the old
// typed-input flow.
func pickMarkdownTarget(ctx context.Context, opts Options) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("plan: getwd: %w", err)
	}
	abs, err := mdfile.ListInDir(cwd)
	if err != nil {
		return "", fmt.Errorf("plan: scan %s: %w", cwd, err)
	}
	if len(abs) == 0 {
		return "", fmt.Errorf("plan: no markdown files in %s (excluding AGENTS.md/README.md)", cwd)
	}
	basenames := make([]string, len(abs))
	byBase := make(map[string]string, len(abs))
	for i, p := range abs {
		base := filepath.Base(p)
		basenames[i] = base
		byBase[base] = p
	}
	chosen, err := opts.UI.PickFromFile(ctx, basenames)
	if err != nil {
		return "", err
	}
	target, ok := byBase[chosen]
	if !ok {
		return "", fmt.Errorf("plan: unknown markdown selection %q", chosen)
	}
	return target, nil
}

// runMarkdown is the markdown-file flow: resolve and read the source,
// pick a tool/model, mint a task ID, ensure `<cwd>/.j/tasks/<id>/`
// exists, and ask the agent to save both the (possibly refined)
// requirements.md and the produced plan.md inside that directory. The
// orchestrator reads both files after agent.Plan returns and updates
// the task summary accordingly. A `planning` task is logged before
// agent.Plan and updated to `plan-done` on success or `help` on
// failure.
func runMarkdown(ctx context.Context, opts Options, rawTarget string) error {
	target, err := mdfile.Resolve(rawTarget)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}

	agent, model, err := selectPlanner(ctx, opts)
	if err != nil {
		return err
	}

	taskID := store.NewTaskID()
	taskDir, err := store.EnsureTaskDir(taskID)
	if err != nil {
		return fmt.Errorf("plan: ensure task dir: %w", err)
	}
	requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
	planPath := filepath.Join(taskDir, store.PlanFileName)

	resumeID, err := agent.NewResumeID(ctx)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "warning: %v\n", err)
	}
	agentLogPath := filepath.Join(taskDir, tasklog.AgentLogFileName)
	mustReadFiles, mustReadErr := mustread.LoadFromDefault()
	if mustReadErr != nil {
		fmt.Fprintf(opts.Stderr, "warning: %v\n", mustReadErr)
	}
	lc := beginPlanTask(opts, agent, model, taskID, target, string(body), resumeID)
	pid, planErr := agent.Plan(ctx, codingagents.PlanRequest{
		FromFilePath:           target,
		Model:                  model,
		RequirementsOutputPath: requirementsPath,
		PlanOutputPath:         planPath,
		Interactive:            *opts.Interactive,
		ResumeChatID:           resumeID,
		AgentLogPath:           agentLogPath,
		MustRead:               mustReadFiles,
	})

	if planErr == nil && pid > 0 {
		lc.recordBackground(pid, agentLogPath)
		fmt.Fprintf(opts.Stdout,
			"J: %s running in background (PID=%d); see .j/tasks/%s/%s\n",
			agent.Name(), pid, taskID, tasklog.AgentLogFileName)
		return nil
	}

	var refinedReq, planMD string
	if planErr == nil {
		if data, readErr := os.ReadFile(requirementsPath); readErr == nil {
			refinedReq = string(data)
		} else {
			fmt.Fprintf(opts.Stderr, "warning: read %s: %v\n", requirementsPath, readErr)
		}
		if data, readErr := os.ReadFile(planPath); readErr == nil {
			planMD = string(data)
		} else {
			fmt.Fprintf(opts.Stderr, "warning: read %s: %v\n", planPath, readErr)
		}
	}
	lc.finishPlan(planErr, refinedReq, planMD, target)
	if planErr != nil {
		return planErr
	}

	fmt.Fprintf(opts.Stdout, "J: the requirements.md and plan.md are saved in .j/tasks/%s/\n", taskID)
	return nil
}

// selectPlanner is the single chokepoint for choosing the planner
// tool/model. Precedence:
//  1. explicit --tool / --model (opts.Tool or opts.Model set) →
//     agentpick.Resolve fills the missing half from the planner
//     bucket and runs CheckLogin. The bucket is NOT written.
//  2. populated planner bucket → agentpick.FromStore reuses it.
//  3. otherwise → agentpick.Pick prompts the user and the result is
//     persisted to the planner bucket.
//
// Settings DB access is short-lived: the bucket is read inside
// plannerFromStore and the handle is released before this returns
// so the agent.Plan call downstream never contends on the bbolt file
// lock.
func selectPlanner(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	if opts.Tool != "" || opts.Model != "" {
		agent, model, err := plannerResolveExplicit(ctx, opts)
		if err == nil {
			return agent, model, nil
		}
		if !errors.Is(err, agentpick.ErrNoStoredSelection) {
			return nil, "", err
		}
	}
	agent, model, err := plannerFromStore(ctx, opts)
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
	persistPlannerSelection(opts, agent.Name(), model)
	return agent, model, nil
}

// plannerResolveExplicit reads the planner bucket only to fill the
// missing half of the user-supplied --tool / --model pair. When
// opts.Store is non-nil it is reused; otherwise this opens
// `<cwd>/.j/settings` for the duration of the read and releases it
// before returning so the file lock is not held across agent.Plan.
func plannerResolveExplicit(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	if opts.Store != nil {
		return agentpick.Resolve(ctx, opts.Store, store.BucketPlanner, opts.Agents, opts.Tool, opts.Model)
	}
	s, ok := openSettingsStore(opts.Stderr)
	if !ok {
		return agentpick.Resolve(ctx, nil, store.BucketPlanner, opts.Agents, opts.Tool, opts.Model)
	}
	defer func() { _ = s.Close() }()
	return agentpick.Resolve(ctx, s, store.BucketPlanner, opts.Agents, opts.Tool, opts.Model)
}

// plannerFromStore reads the planner bucket and returns the chosen
// tool/model. When opts.Store is non-nil (test injection) it is reused
// without any open/close cycle. Otherwise this opens
// `<cwd>/.j/settings` only for the duration of agentpick.FromStore and
// releases it before returning. A failure to open the settings DB
// surfaces as ErrNoStoredSelection so the caller falls back to the
// prompt path the same way an empty bucket would.
func plannerFromStore(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	if opts.Store != nil {
		return agentpick.FromStore(ctx, opts.Store, store.BucketPlanner, opts.Agents)
	}
	s, ok := openSettingsStore(opts.Stderr)
	if !ok {
		return nil, "", agentpick.ErrNoStoredSelection
	}
	defer func() { _ = s.Close() }()
	return agentpick.FromStore(ctx, s, store.BucketPlanner, opts.Agents)
}

// persistPlannerSelection writes the just-confirmed tool/model and
// the interactive flag to the planner bucket. The plan source and the
// markdown source path are intentionally NOT persisted: the user must
// pick those manually each run.
//
// Persistence is best-effort: any error is reported to opts.Stderr
// and otherwise swallowed so plan can keep running. When opts.Store
// is non-nil it is used directly (test injection); otherwise this
// opens `<cwd>/.j/settings` for the duration of the write and
// closes it immediately so the file lock is not held across the
// agent invocation.
func persistPlannerSelection(opts Options, tool, model string) {
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
		store.PersistAgentSelection(opts.Store, opts.Stderr, store.BucketPlanner, tool, model, interactive)
		return
	}
	s, ok := openSettingsStore(opts.Stderr)
	if !ok {
		return
	}
	defer func() { _ = s.Close() }()
	store.PersistAgentSelection(s, opts.Stderr, store.BucketPlanner, tool, model, interactive)
}

// resolveInteractive applies the documented precedence (explicit >
// stored > cobra default true) and returns a concrete bool. Pulled
// out of Run to keep the early-setup block readable and testable in
// isolation.
func resolveInteractive(opts Options) bool {
	if opts.Interactive != nil {
		return *opts.Interactive
	}
	if v, ok := storedPlannerInteractive(opts); ok {
		return v
	}
	return true
}

// storedPlannerInteractive looks up the planner bucket's `interactive`
// entry. When opts.Store is non-nil it is reused; otherwise the
// settings DB is opened and closed solely for this read so the lock
// is not held across the agent call. A failed open or a missing /
// unparseable value yields (_, false) so callers fall back to the
// cobra default.
func storedPlannerInteractive(opts Options) (bool, bool) {
	if opts.Store != nil {
		return agentpick.StoredInteractive(opts.Store, store.BucketPlanner)
	}
	s, ok := openSettingsStore(opts.Stderr)
	if !ok {
		return false, false
	}
	defer func() { _ = s.Close() }()
	return agentpick.StoredInteractive(s, store.BucketPlanner)
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

// openSettingsStore opens `<cwd>/.j/settings` for the planner. It is
// the post-init replacement for store.OpenDefault: pre-flight has
// already created the layout, so failures here are real (e.g.
// concurrent locks) and surface as a single "warning: ..." line on
// stderr. Best-effort by design; nil store callers (no recorded
// selection) are tolerated downstream.
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
