// Package plan implements the `j plan` subcommand. It collects a planning
// source (markdown or linear), asks for a model and a coding
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


	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/tasklog"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/util/mdfile"
	"github.com/spacelions/j/internal/util/run"
)

// UI is the slice of picker methods `j plan` calls. *picker.Picker
// satisfies it via duck typing; tests inject scripted fakes.
type UI interface {
	SelectSource(ctx context.Context, allowed []picker.Source) (picker.Source, error)
	PickMarkdownInCwd(ctx context.Context) (string, error)
	PickTask(ctx context.Context, title string, tasks []store.Task) (string, bool, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
}

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
	// Interactive is the resolved interactive flag. cobra cmd.go
	// computes it via resolver.Interactive (explicit > stored > true)
	// before constructing Options.
	Interactive bool

	// Tool and Model are one-off overrides for the planner bucket's
	// recorded tool/model. When either is set, Run resolves the
	// planner via resolver.Agent (filling the missing half from
	// the bucket if needed) and skips persistence: the bucket is
	// left untouched. When both are empty, Run falls back to the
	// existing read-then-prompt-then-persist precedence.
	Tool  string
	Model string

	// WaitForCompletion blocks on a returned non-zero PID and runs
	// finishPlan synchronously, instead of leaving the row at
	// `planning` for the `j tasks` reaper. Used by the orchestrator
	// chain so the next phase only fires after the planner exits.
	WaitForCompletion bool

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
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("plan: no coding agents configured")
	}
	// --from-task short-circuits the source picker; the explicit
	// flag is unambiguous so we head straight to the re-plan
	// flow without prompting for a source.
	if opts.TaskID != "" {
		return runReplanTask(ctx, opts, opts.TaskID)
	}
	if opts.FromFile != "" {
		return runMarkdown(ctx, opts, opts.FromFile)
	}

	res, err := picker.PickSource(ctx, opts.UI,
		[]picker.Source{picker.SourceMarkdown, picker.SourceLinear, picker.SourceTask},
		func() ([]store.Task, error) { return listAllTasks(opts) },
		errors.New("plan: no tasks to re-plan; run `j plan` first"))
	if err != nil {
		return err
	}
	if res.Cancelled {
		return nil
	}
	switch res.Source {
	case picker.SourceMarkdown:
		return runMarkdown(ctx, opts, res.Markdown)
	case picker.SourceLinear:
		fmt.Fprintln(opts.Stdout, "plan: linear source is not yet wired up; nothing to do")
		return nil
	case picker.SourceTask:
		return runReplanTask(ctx, opts, res.TaskID)
	}
	return fmt.Errorf("plan: unsupported source %s", res.Source)
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
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		fmt.Fprintf(opts.Stderr, "warning: %v\n", mustReadErr)
	}
	lc := beginPlanTaskReuse(opts, agent, model, existing, resumeID)
	pid, planErr := agent.Plan(ctx, codingagents.PlanRequest{
		FromFilePath:           requirementsPath,
		Model:                  model,
		RequirementsOutputPath: requirementsPath,
		PlanOutputPath:         planPath,
		Interactive:            opts.Interactive,
		ResumeChatID:           resumeID,
		AgentLogPath:           agentLogPath,
		MustRead:               mustReadFiles,
	})

	if planErr == nil && pid > 0 {
		if opts.WaitForCompletion {
			if err := run.WaitForExit(ctx, pid); err != nil {
				lc.finishPlan(err, "", "", requirementsPath)
				return err
			}
		} else {
			lc.recordBackground(pid, agentLogPath)
			fmt.Fprintf(opts.Stdout,
				"J: %s running in background (PID=%d); see .j/tasks/%s/%s\n",
				agent.Name(), pid, existing.ID, tasklog.AgentLogFileName)
			return nil
		}
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
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		fmt.Fprintf(opts.Stderr, "warning: %v\n", mustReadErr)
	}
	lc := beginPlanTask(opts, agent, model, taskID, target, string(body), resumeID)
	pid, planErr := agent.Plan(ctx, codingagents.PlanRequest{
		FromFilePath:           target,
		Model:                  model,
		RequirementsOutputPath: requirementsPath,
		PlanOutputPath:         planPath,
		Interactive:            opts.Interactive,
		ResumeChatID:           resumeID,
		AgentLogPath:           agentLogPath,
		MustRead:               mustReadFiles,
	})

	if planErr == nil && pid > 0 {
		if opts.WaitForCompletion {
			if err := run.WaitForExit(ctx, pid); err != nil {
				lc.finishPlan(err, "", "", target)
				return err
			}
		} else {
			lc.recordBackground(pid, agentLogPath)
			fmt.Fprintf(opts.Stdout,
				"J: %s running in background (PID=%d); see .j/tasks/%s/%s\n",
				agent.Name(), pid, taskID, tasklog.AgentLogFileName)
			return nil
		}
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

// selectPlanner delegates to resolver.Agent with the planner bucket.
// Resolver owns the precedence chain (explicit → stored → prompt) and
// the persist step; the cli only forwards inputs.
func selectPlanner(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	return resolver.Agent(ctx, resolver.AgentOptions{
		Bucket:        store.BucketPlanner,
		Agents:        opts.Agents,
		ExplicitTool:  opts.Tool,
		ExplicitModel: opts.Model,
		UI:            opts.UI,
		Store:         opts.Store,
		Stderr:        opts.Stderr,
		Interactive:   opts.Interactive,
	})
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
		o.UI = picker.New(o.Stdin, o.Stderr)
	}
	return o
}

