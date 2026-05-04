package tasks

import (
	"context"
	"errors"
	"io"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/cli/picker"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
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
// in order. For each bucket it calls resolver.AgentFromStore; on
// resolver.ErrNoStoredSelection it prompts via picker.PickAgent and
// persists the result with interactive=true.
//
// The bbolt file lock is released between buckets so concurrent
// `j tasks` / `j settings` calls in another shell never block.
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

func ensureBucketSelection(ctx context.Context, opts AgentCheckOptions, bucket string) error {
	_, _, err := readBucketSelection(ctx, opts, bucket)
	if err == nil {
		return nil
	}
	if !errors.Is(err, resolver.ErrNoStoredSelection) {
		return err
	}
	banner.Fprintf(opts.Stderr, "J: Choose your favourite for %s:\n", bucket)
	pickedAgent, pickedModel, err := picker.PickAgent(ctx, opts.UI, opts.Agents)
	if err != nil {
		return err
	}
	persistBucketSelection(opts, bucket, pickedAgent.Name(), pickedModel)
	return nil
}

// readBucketSelection opens settings, calls resolver.AgentFromStore,
// closes settings before returning. A settings-open failure surfaces
// as ErrNoStoredSelection so callers fall through to the prompt path.
func readBucketSelection(ctx context.Context, opts AgentCheckOptions, bucket string) (codingagents.Agent, string, error) {
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return nil, "", resolver.ErrNoStoredSelection
	}
	defer func() { _ = s.Close() }()
	return resolver.AgentFromStore(ctx, s, bucket, opts.Agents)
}

// persistBucketSelection writes the prompt result into the bucket
// with interactive=true. Persistence is best-effort: a settings open
// failure is warned to stderr and otherwise swallowed so the user's
// pick is not lost on a transient lock error.
func persistBucketSelection(opts AgentCheckOptions, bucket, tool, model string) {
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return
	}
	defer func() { _ = s.Close() }()
	store.PersistAgentSelection(s, opts.Stderr, bucket, tool, model, true)
}

func (o AgentCheckOptions) withDefaults() AgentCheckOptions {
	if o.UI == nil {
		o.UI = picker.New(o.Stdin, o.Stderr)
	}
	return o
}
