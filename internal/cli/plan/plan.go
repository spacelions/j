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

	codingagents "github.com/spacelions/j/internal/coding-agents"
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

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI
}

// Run executes `j plan`. When Options.Target is set it goes straight to
// the markdown source (preserving --target/PLAN_TARGET semantics).
// Otherwise it asks the user which source to use and dispatches.
func Run(ctx context.Context, opts Options) error {
	opts = opts.withDefaults()
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
// to surface success or a "was not written" warning.
func runMarkdown(ctx context.Context, opts Options, rawTarget string) error {
	target, err := resolveTarget(rawTarget)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("read target: %w", err)
	}

	agent, model, err := pickAgentAndModel(ctx, opts)
	if err != nil {
		return err
	}

	out := planOutputPath(target)
	if err := agent.Plan(ctx, codingagents.PlanRequest{
		TargetPath:  target,
		Body:        string(body),
		Model:       model,
		OutputPath:  out,
		Interactive: opts.Interactive,
	}); err != nil {
		return err
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
// the agent.
func runScratch(ctx context.Context, opts Options) error {
	agent, model, err := pickAgentAndModel(ctx, opts)
	if err != nil {
		return err
	}
	return agent.Plan(ctx, codingagents.PlanRequest{
		Model:       model,
		Interactive: true,
	})
}

// pickAgentAndModel walks the shared tool/model/login prompts. Both
// markdown and scratch flows need the same three steps; lifting them
// keeps Run small and prevents the two flows from drifting apart.
func pickAgentAndModel(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	names := make([]string, len(opts.Agents))
	for i, a := range opts.Agents {
		names[i] = a.Name()
	}
	chosen, err := opts.UI.SelectTool(ctx, names)
	if err != nil {
		return nil, "", err
	}
	agent, ok := lookupAgent(opts.Agents, chosen)
	if !ok {
		return nil, "", fmt.Errorf("unknown tool %q", chosen)
	}

	models, err := agent.ListModels(ctx)
	if err != nil {
		return nil, "", err
	}
	model, err := opts.UI.SelectModel(ctx, models)
	if err != nil {
		return nil, "", err
	}

	if err := agent.CheckLogin(ctx); err != nil {
		return nil, "", err
	}
	return agent, model, nil
}

func lookupAgent(agents []codingagents.Agent, name string) (codingagents.Agent, bool) {
	for _, a := range agents {
		if a.Name() == name {
			return a, true
		}
	}
	return nil, false
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
	return o
}
