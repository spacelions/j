package preflight

import (
	"context"
	"errors"
	"io"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/uitheme"
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

// AgentSelector aliases picker.Selector so call sites (e.g. tasks
// start/continue options) can name a narrow interface without
// embedding picker in their public struct docs.
type AgentSelector = picker.Selector

// EnsureAgentSelections walks the planner / worker / verifier buckets
// in order. For each bucket it calls resolver.AgentFromStore; on
// resolver.ErrNoStoredSelection it prompts via picker.PickAgent and
// persists the durable tool/model result.
//
// The bbolt file lock is released between buckets so concurrent
// `j tasks` / `j settings` calls in another shell never block.
// huh.ErrUserAborted from the Selector propagates verbatim so the
// caller (tasks.RunStart / tasks.RunContinue) can treat a Ctrl-C as a
// clean cancel via its existing deferred guard.
func EnsureAgentSelections(ctx context.Context, opts AgentCheckOptions) error {
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("preflight: no coding agents configured")
	}
	for _, bucket := range []string{
		store.BucketPlanner,
		store.BucketWorker,
		store.BucketVerifier,
	} {
		if err := ensureBucketSelection(ctx, opts, bucket); err != nil {
			return err
		}
	}
	return nil
}

func ensureBucketSelection(
	ctx context.Context, opts AgentCheckOptions, bucket string,
) error {
	_, _, err := readBucketSelection(ctx, opts, bucket)
	if err == nil {
		return nil
	}
	if !errors.Is(err, resolver.ErrNoStoredSelection) {
		return err
	}
	uitheme.NormalFprintf(
		opts.Stderr, "J: Choose your favourite for %s:\n", bucket,
	)
	pickedAgent, pickedModel, err := picker.PickAgent(ctx, opts.UI, opts.Agents)
	if err != nil {
		return err
	}
	persistBucketSelection(opts, bucket, pickedAgent.Name(), pickedModel)
	return nil
}

func readBucketSelection(
	ctx context.Context, opts AgentCheckOptions, bucket string,
) (codingagents.Agent, string, error) {
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return nil, "", resolver.ErrNoStoredSelection
	}
	defer func() { _ = s.Close() }()
	return resolver.AgentFromStore(ctx, s, bucket, opts.Agents)
}

func persistBucketSelection(
	opts AgentCheckOptions, bucket, tool, model string,
) {
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return
	}
	defer func() { _ = s.Close() }()
	store.PersistAgentSelection(s, opts.Stderr, bucket, tool, model)
}

func (o AgentCheckOptions) withDefaults() AgentCheckOptions {
	if o.UI == nil {
		o.UI = picker.New(o.Stdin, o.Stderr)
	}
	return o
}
