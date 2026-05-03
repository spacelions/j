package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/cli/agentpick"
	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// AgentCheckOptions configures EnsureAgentSelections. Stdin/Stdout/Stderr
// default to the process streams; UI defaults to a huh-backed selector
// that satisfies agentpick.Selector. Agents must be supplied so the
// helper can validate the chosen tool exists in the wired set.
type AgentCheckOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     AgentSelector
}

// AgentSelector is the slice of UI surface EnsureAgentSelections needs.
// It mirrors agentpick.Selector verbatim so any implementation usable by
// `j plan` / `j work` / `j verify` can be reused here without an extra
// adapter; the dedicated alias keeps the type direction CLI -> agentpick
// (not the reverse) the same way agentpick.Selector itself is defined
// locally to that package.
type AgentSelector interface {
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
}

// EnsureAgentSelections walks the planner / worker / verifier buckets
// in order. For each bucket it:
//
//  1. Opens `<cwd>/.j/settings`.
//  2. Calls agentpick.FromStore. If the bucket already carries a
//     valid tool/model pair the helper closes the store and moves to
//     the next bucket without prompting.
//  3. On agentpick.ErrNoStoredSelection it closes the store, runs
//     agentpick.Pick against opts.UI, and re-opens the store to
//     persist the selection via store.PersistAgentSelection. The
//     persisted `interactive` flag defaults to true (resume reads
//     this on every run; the explicit user choice flows through
//     the parent commands' --interactive flag).
//  4. Closes the store before returning to the caller.
//
// The store is intentionally never held across a Pick prompt so the
// bbolt file lock is released between buckets and concurrent
// `j tasks` / `j settings` calls in another shell never block.
//
// huh.ErrUserAborted from the Selector propagates verbatim so the
// caller (RunStart / RunContinue) can treat a Ctrl-C as a clean
// cancel via its existing deferred guard.
func EnsureAgentSelections(ctx context.Context, opts AgentCheckOptions) error {
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("tasks: no coding agents configured")
	}
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		if err := ensureBucketSelection(ctx, opts, bucket); err != nil {
			return err
		}
	}
	return nil
}

// ensureBucketSelection encapsulates the per-bucket lifecycle. The
// store is opened, queried, and closed once (FromStore happy path) or
// twice (read + write) per bucket so the lock is never held across the
// Pick prompt.
func ensureBucketSelection(ctx context.Context, opts AgentCheckOptions, bucket string) error {
	agent, model, err := readBucketSelection(ctx, opts, bucket)
	if err == nil {
		_ = agent
		_ = model
		return nil
	}
	if !errors.Is(err, agentpick.ErrNoStoredSelection) {
		return fmt.Errorf("tasks: read %s: %w", bucket, err)
	}
	fmt.Fprintf(opts.Stderr, "Choose your favourite for %s:\n", bucket)
	pickedAgent, pickedModel, err := agentpick.Pick(ctx, opts.UI, opts.Agents)
	if err != nil {
		return err
	}
	return persistBucketSelection(opts, bucket, pickedAgent.Name(), pickedModel)
}

// readBucketSelection opens settings, runs agentpick.FromStore, and
// closes settings before returning. A failure to open the settings DB
// surfaces as ErrNoStoredSelection so the caller falls back to the
// prompt path the same way an empty bucket would.
func readBucketSelection(ctx context.Context, opts AgentCheckOptions, bucket string) (codingagents.Agent, string, error) {
	s, ok := openSettingsStore(opts.Stderr)
	if !ok {
		return nil, "", agentpick.ErrNoStoredSelection
	}
	defer func() { _ = s.Close() }()
	return agentpick.FromStore(ctx, s, bucket, opts.Agents)
}

// persistBucketSelection re-opens settings only for the duration of the
// write. The `interactive` flag is recorded as true so resume runs read
// a sensible default; users that want headless resumes set it via the
// parent commands' --interactive flag (which writes the same key).
// Persistence is best-effort: a settings open failure is reported via
// stderr by openSettingsStore and otherwise swallowed so the user's
// just-confirmed pick is not lost on a transient lock error.
func persistBucketSelection(opts AgentCheckOptions, bucket, tool, model string) error {
	s, ok := openSettingsStore(opts.Stderr)
	if !ok {
		return nil
	}
	defer func() { _ = s.Close() }()
	store.PersistAgentSelection(s, opts.Stderr, bucket, tool, model, true)
	return nil
}

// openSettingsStore opens `<cwd>/.j/settings` and reports a single
// "warning: ..." line on stderr if the open fails. The pattern matches
// plan / work / verify openSettingsStore so the surfacing is uniform.
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

// huhAgentSelector is the huh-backed implementation of AgentSelector.
// It is the default UI when EnsureAgentSelections is not given an
// explicit selector. Tests substitute a scripted fake.
type huhAgentSelector struct {
	in  io.Reader
	out io.Writer
}

func newHuhAgentSelector(in io.Reader, out io.Writer) *huhAgentSelector {
	return &huhAgentSelector{in: in, out: out}
}

func (u *huhAgentSelector) SelectTool(ctx context.Context, options []string) (string, error) {
	return u.choose(ctx, "Select coding agent tool", options)
}

func (u *huhAgentSelector) SelectModel(ctx context.Context, options []string) (string, error) {
	return u.choose(ctx, "Select model", options)
}

func (u *huhAgentSelector) choose(ctx context.Context, title string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("tasks: %s: no options available", title)
	}
	var v string
	err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(title).
			Options(huh.NewOptions(options...)...).
			Filtering(true).
			Value(&v),
	)).WithInput(u.in).WithOutput(u.out).WithTheme(uitheme.Theme()).RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return "", huh.ErrUserAborted
	}
	if err != nil {
		return "", fmt.Errorf("tasks ui: %w", err)
	}
	return v, nil
}

func (o AgentCheckOptions) withDefaults() AgentCheckOptions {
	if o.UI == nil {
		o.UI = newHuhAgentSelector(o.Stdin, o.Stderr)
	}
	return o
}
