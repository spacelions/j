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
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/cli/picker"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
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
// semantics). Otherwise it asks which source to use and dispatches.
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
		listAllTasks,
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
		banner.Fprintln(opts.Stdout, "J: plan linear source is not yet wired up; nothing to do")
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
	existing, err := loadTaskByID(id)
	if err != nil {
		return err
	}
	proceed, err := resolver.ConfirmStatusOverride(ctx, opts.UI, opts.Yes, "re-plan", existing, resolver.ReplanAllowed)
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
		banner.DangerousBox(opts.Stderr, "J: %v", err)
	}
	agentLogPath := filepath.Join(taskDir, store.AgentLogFileName)
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		banner.DangerousBox(opts.Stderr, "J: %v", mustReadErr)
	}
	lc := existing.BeginPlanReuse(opts.Stderr, agent.Name(), model, resumeID)
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
				lc.Finish(err, "", "", requirementsPath)
				return err
			}
		} else {
			lc.RecordBackground(pid, agentLogPath)
			banner.RunningInBackground(opts.Stdout, agent.Name(), pid, agentLogPath)
			return nil
		}
	}

	var refinedReq, planMD string
	if planErr == nil {
		if data, readErr := os.ReadFile(requirementsPath); readErr == nil {
			refinedReq = string(data)
		} else {
			banner.DangerousBox(opts.Stderr, "J: read %s: %v", requirementsPath, readErr)
		}
		if data, readErr := os.ReadFile(planPath); readErr == nil {
			planMD = string(data)
		} else {
			banner.DangerousBox(opts.Stderr, "J: read %s: %v", planPath, readErr)
		}
	}
	lc.Finish(planErr, refinedReq, planMD, requirementsPath)
	if planErr != nil {
		return planErr
	}

	banner.Fprintf(opts.Stdout, "J: re-planned task %s\n", existing.ID)
	return nil
}

func loadTaskByID(id string) (store.Task, error) {
	return resolver.TaskByID("plan", id)
}

func listAllTasks() ([]store.Task, error) {
	return resolver.ListTasks("plan")
}

func runMarkdown(ctx context.Context, opts Options, rawTarget string) error {
	source, err := resolver.ResolvePlanMarkdown(rawTarget)
	if err != nil {
		return err
	}
	agent, model, err := selectPlanner(ctx, opts)
	if err != nil {
		return err
	}
	return resolver.RunPlanMarkdown(ctx, resolver.PlanMarkdownOptions{
		Source:            source,
		Stdout:            opts.Stdout,
		Stderr:            opts.Stderr,
		Agent:             agent,
		Model:             model,
		Interactive:       opts.Interactive,
		WaitForCompletion: opts.WaitForCompletion,
	})
}

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
