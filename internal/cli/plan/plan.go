// Package plan implements the `j plan` subcommand. It collects a planning
// source (markdown today, linear later), asks for a model and a coding
// agent backend, verifies that backend is signed in, and runs it to
// produce a refined requirements summary and a plan stored under
// <cwd>/.j/tasks/<id>/. No file is written to the workspace; `j tasks`
// lists the runs and `j work --task <id>` executes the plan.
package plan

import (
	"context"
	"errors"
	"fmt"
	"io"
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
// scripted ones). Interactive selects the agent's TUI when true and the
// headless capture path when false.
type Options struct {
	// FromFile is the markdown task description path (from --from-file
	// or PLAN_FROM_FILE). When empty the orchestrator prompts via the
	// UI.
	FromFile    string
	Interactive bool

	// FromSettings, when true, makes Run reuse the tool/model
	// recorded in the planner bucket of <cwd>/.j/settings instead of
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
	// tool/model/interactive flag last used (the plan source and the
	// markdown source path are intentionally NOT persisted). The
	// orchestrator does not own the lifecycle: callers that supply a
	// Store keep the lifecycle. When nil, withDefaults opens the
	// default <cwd>/.j/settings DB and closes it after Run returns.
	Store *store.Store

	// closeStore is set internally by withDefaults when it allocates
	// the default Store, so Run can close it before returning. Tests
	// that pass their own Store leave this false.
	closeStore bool
}

// Run executes `j plan`. When Options.FromFile is set it goes straight
// to the markdown source (preserving --from-file/PLAN_FROM_FILE
// semantics). Otherwise it asks the user which source to use and
// dispatches.
func Run(ctx context.Context, opts Options) error {
	opts = opts.withDefaults()
	if opts.closeStore && opts.Store != nil {
		defer func() { _ = opts.Store.Close() }()
	}
	if len(opts.Agents) == 0 {
		return errors.New("plan: no coding agents configured")
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
		raw, err := opts.UI.AskFromFile(ctx)
		if err != nil {
			return err
		}
		return runMarkdown(ctx, opts, raw)
	case SourceLinear:
		fmt.Fprintln(opts.Stdout, "plan: linear source is not yet wired up; nothing to do")
		return nil
	}
	return fmt.Errorf("plan: unsupported source %s", src)
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
	lc := beginPlanTask(opts, agent, model, taskID, target, string(body), resumeID)
	planErr := agent.Plan(ctx, codingagents.PlanRequest{
		FromFilePath:           target,
		Body:                   string(body),
		Model:                  model,
		RequirementsOutputPath: requirementsPath,
		PlanOutputPath:         planPath,
		Interactive:            opts.Interactive,
		ResumeChatID:           resumeID,
	})

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

	fmt.Fprintf(opts.Stdout, "plan recorded as task %s\n", taskID)
	return nil
}

// selectPlanner is the single chokepoint for choosing the planner
// tool/model. When FromSettings is true it tries the read-only
// agentpick.FromStore path first and only falls back to the
// interactive Pick flow on ErrNoStoredSelection (printing a single
// stderr line so the user knows why they're being prompted). The
// just-confirmed selection is persisted only on the prompted path:
// values that came from the store are already there.
func selectPlanner(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	if opts.FromSettings {
		agent, model, err := agentpick.FromStore(ctx, opts.Store, store.BucketPlanner, opts.Agents)
		if err == nil {
			return agent, model, nil
		}
		if !errors.Is(err, agentpick.ErrNoStoredSelection) {
			return nil, "", err
		}
		fmt.Fprintln(opts.Stderr, "no stored planner selection; prompting")
	}
	agent, model, err := agentpick.Pick(ctx, opts.UI, opts.Agents)
	if err != nil {
		return nil, "", err
	}
	persistPlannerSelection(opts, agent.Name(), model)
	return agent, model, nil
}

// persistPlannerSelection writes the just-confirmed tool/model and
// the interactive flag to the planner bucket. The plan source and the
// markdown source path are intentionally NOT persisted: the user must
// pick those manually each run.
//
// Persistence is best-effort: any error is reported to opts.Stderr
// and otherwise swallowed so plan can keep running. When opts.Store
// is nil this is a no-op.
func persistPlannerSelection(opts Options, tool, model string) {
	store.PersistAgentSelection(opts.Store, opts.Stderr, store.BucketPlanner, tool, model, opts.Interactive)
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
