// Package plan implements the `j plan` subcommand. It collects a markdown
// target and a model from the user, picks a coding-agent backend (Cursor
// today, Codex/Claude later), verifies that backend is signed in, runs it
// in plan mode, and writes the resulting plan to plan.md beside the
// target markdown file.
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
// headless capture path when false.
type Options struct {
	Target      string
	Interactive bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI
}

// Run executes `j plan`.
func Run(ctx context.Context, opts Options) error {
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("plan: no coding agents configured")
	}

	rawTarget := opts.Target
	if rawTarget == "" {
		v, err := opts.UI.AskTarget(ctx)
		if err != nil {
			return err
		}
		rawTarget = v
	}

	target, err := resolveTarget(rawTarget)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("read target: %w", err)
	}

	names := make([]string, len(opts.Agents))
	for i, a := range opts.Agents {
		names[i] = a.Name()
	}
	chosen, err := opts.UI.SelectTool(ctx, names)
	if err != nil {
		return err
	}
	agent, ok := lookupAgent(opts.Agents, chosen)
	if !ok {
		return fmt.Errorf("unknown tool %q", chosen)
	}

	models, err := agent.ListModels(ctx)
	if err != nil {
		return err
	}
	model, err := opts.UI.SelectModel(ctx, models)
	if err != nil {
		return err
	}

	if err := agent.CheckLogin(ctx); err != nil {
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
