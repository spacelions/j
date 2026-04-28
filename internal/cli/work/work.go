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
	"github.com/spacelions/j/internal/store"
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
	// tool/model/interactive flag last used. The work source (the
	// plan file path) is intentionally NOT persisted: the user must
	// supply or be prompted for it every run. The orchestrator does
	// not own the lifecycle when the caller supplies a Store. When
	// nil, withDefaults opens the default <cwd>/.j/settings DB and
	// closes it after Run returns.
	Store *store.Store

	// closeStore is set internally by withDefaults when it allocates
	// the default Store, so Run can close it before returning. Tests
	// that pass their own Store leave this false.
	closeStore bool
}

// Run executes `j work`. When Options.Target is set it goes straight to
// resolution; otherwise it asks the user for the plan path.
func Run(ctx context.Context, opts Options) error {
	opts = opts.withDefaults()
	if opts.closeStore && opts.Store != nil {
		defer func() { _ = opts.Store.Close() }()
	}
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

	agent, model, err := selectCoder(ctx, opts)
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
		if s, ok := store.OpenDefault(o.Stderr, store.BucketCoder); ok {
			o.Store = s
			o.closeStore = true
		}
	}
	return o
}
