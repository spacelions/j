// Package work implements the `j work` subcommand. It takes an existing
// plan markdown file (or asks for one), prompts the user for a coding
// agent and model, verifies that backend is signed in, and hands the
// plan to the agent so it can execute the plan against the plan's
// directory. The orchestrator does not write any output file: the
// coder edits files in place.
package work

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spacelions/j/internal/cli/agentpick"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/util/mdfile"
)

// Options configures Run. Stdin/Stdout/Stderr default to the process
// streams. UI defaults to the huh implementation. Agents must be
// supplied by the caller (the CLI wires the Cursor agent; tests inject
// scripted ones). Interactive selects the agent's TUI when true and
// the headless path when false.
type Options struct {
	Target      string
	Interactive bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI
}

// Run executes `j work`. When Options.Target is set it goes straight to
// resolution; otherwise it asks the user for the plan path.
func Run(ctx context.Context, opts Options) error {
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("work: no coding agents configured")
	}

	raw := opts.Target
	if raw == "" {
		v, err := opts.UI.AskTarget(ctx)
		if err != nil {
			return err
		}
		raw = v
	}

	plan, err := mdfile.Resolve(raw)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(plan)
	if err != nil {
		return fmt.Errorf("read plan: %w", err)
	}

	agent, model, err := agentpick.Pick(ctx, opts.UI, opts.Agents)
	if err != nil {
		return err
	}

	if err := agent.Work(ctx, codingagents.WorkRequest{
		PlanPath:    plan,
		Body:        string(body),
		Model:       model,
		Interactive: opts.Interactive,
	}); err != nil {
		return err
	}

	fmt.Fprintf(opts.Stdout, "coding against %s\n", plan)
	return nil
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
