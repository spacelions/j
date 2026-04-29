// Package plan implements the `j plan` subcommand. It collects a planning
// source, asks for a model and a coding-agent backend (Cursor today,
// Codex/Claude later), verifies that backend is signed in, and runs it.
// For markdown sources it writes <stem>.plan.md beside the input; for
// from-scratch sources it just hands the agent's plan-mode TUI to the
// user; the linear source is a stub today.
package plan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spacelions/j/internal/cli/agentpick"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/util/mdfile"
)

// Options configures Run. Stdin/Stdout/Stderr default to the process
// streams. UI defaults to the huh implementation. Agents must be
// supplied by the caller (the CLI wires the Cursor agent; tests inject
// scripted ones). Interactive selects the agent's TUI when true and the
// headless capture path when false; it only affects the markdown source
// because scratch is always a TUI session and linear runs no agent.
type Options struct {
	Target      string
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
	// target path are intentionally NOT persisted). The orchestrator
	// does not own the lifecycle: callers that supply a Store keep
	// the lifecycle. When nil, withDefaults opens the default
	// <cwd>/.j/settings DB and closes it after Run returns.
	Store *store.Store

	// closeStore is set internally by withDefaults when it allocates
	// the default Store, so Run can close it before returning. Tests
	// that pass their own Store leave this false.
	closeStore bool
}

// Run executes `j plan`. When Options.Target is set it goes straight to
// the markdown source (preserving --target/PLAN_TARGET semantics).
// Otherwise it asks the user which source to use and dispatches.
func Run(ctx context.Context, opts Options) error {
	opts = opts.withDefaults()
	if opts.closeStore && opts.Store != nil {
		defer func() { _ = opts.Store.Close() }()
	}
	if len(opts.Agents) == 0 {
		return errors.New("plan: no coding agents configured")
	}

	if opts.Target != "" {
		return runMarkdown(ctx, opts, opts.Target)
	}

	src, err := opts.UI.SelectSource(ctx)
	if err != nil {
		return err
	}
	switch src {
	case SourceMarkdown:
		raw, err := opts.UI.AskTarget(ctx)
		if err != nil {
			return err
		}
		return runMarkdown(ctx, opts, raw)
	case SourceScratch:
		return runScratch(ctx, opts)
	case SourceLinear:
		fmt.Fprintln(opts.Stdout, "plan: linear source is not yet wired up; nothing to do")
		return nil
	}
	return fmt.Errorf("plan: unsupported source %s", src)
}

// runMarkdown is the original markdown-file flow: resolve and read the
// target, pick a tool/model, verify login, and ask the agent to produce
// <stem>.plan.md. The agent owns the file write; we just stat it after
// to surface success or a "was not written" warning. A `planning` task
// is logged before agent.Plan and updated to `planned` (with the
// produced plan body attached) on success or `help` on failure.
func runMarkdown(ctx context.Context, opts Options, rawTarget string) error {
	target, err := mdfile.Resolve(rawTarget)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("read target: %w", err)
	}

	agent, model, err := selectPlanner(ctx, opts)
	if err != nil {
		return err
	}

	out := planOutputPath(target)
	lc := beginPlanTask(opts, agent, model, target, string(body))
	planErr := agent.Plan(ctx, codingagents.PlanRequest{
		TargetPath:  target,
		Body:        string(body),
		Model:       model,
		OutputPath:  out,
		Interactive: opts.Interactive,
	})
	var planMD string
	if planErr == nil {
		if data, readErr := os.ReadFile(out); readErr == nil {
			planMD = string(data)
		}
	}
	lc.finishPlan(planErr, planMD)
	if planErr != nil {
		return planErr
	}

	if _, err := os.Stat(out); err == nil {
		fmt.Fprintf(opts.Stdout, "wrote %s\n", out)
	} else {
		fmt.Fprintf(opts.Stderr, "warning: %s was not written\n", out)
	}
	return nil
}

// runScratch hands the agent's plan-mode TUI to the user with no
// markdown body and no expected output file. The empty TargetPath +
// OutputPath in the request is the contract that signals scratch to
// the agent. A `planning` task is still logged so `j tasks` reflects
// every real plan run.
func runScratch(ctx context.Context, opts Options) error {
	agent, model, err := selectPlanner(ctx, opts)
	if err != nil {
		return err
	}
	lc := beginPlanTask(opts, agent, model, "", "")
	planErr := agent.Plan(ctx, codingagents.PlanRequest{
		Model:       model,
		Interactive: true,
	})
	lc.finishPlan(planErr, "")
	return planErr
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
// the interactive flag to the planner bucket. The plan source
// (markdown/scratch/linear) and the target path are intentionally
// NOT persisted: the user must pick those manually each run.
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
		if s, ok := store.OpenDefault(o.Stderr, store.BucketPlanner); ok {
			o.Store = s
			o.closeStore = true
		}
	}
	return o
}
