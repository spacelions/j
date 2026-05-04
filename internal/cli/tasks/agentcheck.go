package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spacelions/j/internal/cli/picker"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// AgentCheckOptions configures EnsureAgentSelections. Stdin/Stdout/Stderr
// default to the process streams; UI defaults to picker.New. Agents
// must be supplied so the helper can validate the chosen tool exists
// in the wired set.
type AgentCheckOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     AgentSelector
}

// AgentSelector aliases picker.Selector so existing callers
// (start.go's StartOptions, continue.go) keep their tasks-package type
// reference without the extra hop through picker.
type AgentSelector = picker.Selector

// EnsureAgentSelections walks the planner / worker / verifier buckets
// in order. For each bucket it:
//
//  1. Opens `<cwd>/.j/settings`.
//  2. Calls picker.AgentFromStore. If the bucket already carries a
//     valid tool/model pair the helper closes the store and moves to
//     the next bucket without prompting.
//  3. On picker.ErrNoStoredSelection it closes the store, runs
//     picker.PickAgent against opts.UI, and re-opens the store to
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
	if !errors.Is(err, picker.ErrNoStoredSelection) {
		return err
	}
	fmt.Fprintf(opts.Stderr, "Choose your favourite for %s:\n", bucket)
	pickedAgent, pickedModel, err := picker.PickAgent(ctx, opts.UI, opts.Agents)
	if err != nil {
		return err
	}
	return persistBucketSelection(opts, bucket, pickedAgent.Name(), pickedModel)
}

// readBucketSelection opens settings, runs picker.AgentFromStore, and
// closes settings before returning. A failure to open the settings DB
// surfaces as ErrNoStoredSelection so the caller falls back to the
// prompt path the same way an empty bucket would.
func readBucketSelection(ctx context.Context, opts AgentCheckOptions, bucket string) (codingagents.Agent, string, error) {
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return nil, "", picker.ErrNoStoredSelection
	}
	defer func() { _ = s.Close() }()
	return picker.AgentFromStore(ctx, s, bucket, opts.Agents)
}

// persistBucketSelection re-opens settings only for the duration of the
// write. The `interactive` flag is recorded as true so resume runs read
// a sensible default; users that want headless resumes set it via the
// parent commands' --interactive flag (which writes the same key).
// Persistence is best-effort: a settings open failure is reported via
// stderr by store.OpenSettings and otherwise swallowed so the user's
// pick is not lost on a transient lock error.
func persistBucketSelection(opts AgentCheckOptions, bucket, tool, model string) error {
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return nil
	}
	defer func() { _ = s.Close() }()
	store.PersistAgentSelection(s, opts.Stderr, bucket, tool, model, true)
	return nil
}

func (o AgentCheckOptions) withDefaults() AgentCheckOptions {
	if o.UI == nil {
		o.UI = picker.New(o.Stdin, o.Stderr)
	}
	return o
}
