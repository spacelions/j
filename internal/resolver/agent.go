// Package resolver concentrates the read / fallback / persist logic for
// every bbolt bucket the j subcommands consume. Each cli command (plan,
// work, verify, tasks/start, preflight agent selection, settings, init) calls
// into resolver to translate "what should I run" into a concrete value,
// independent of how the value was supplied (cli flag, stored bucket,
// interactive prompt). The package is UI-free; it consumes
// picker.Selector for the prompt path so cli scripts can inject test
// fakes through their narrow UI interface.
package resolver

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// ErrNoStoredSelection signals that the bucket does not yet hold a
// usable tool/model pair. Callers fall back to the interactive prompt
// branch in resolver.Agent on first runs.
var ErrNoStoredSelection = errors.New("resolver: no stored selection")

// AgentOptions bundles the inputs resolver.Agent needs. Populating it
// per call site keeps each cli's selectXxx body to a single function
// call.
type AgentOptions struct {
	// Bucket is one of store.BucketPlanner / BucketWorker /
	// BucketVerifier. Resolver reads "tool" / "model" / "interactive"
	// keys from it and writes them back on the prompt branch.
	Bucket string

	// Agents is the cli-supplied set of available coding agents. The
	// returned agent is one of these.
	Agents []codingagents.Agent

	// ExplicitTool / ExplicitModel are the --tool / --model flags. When
	// either is non-empty resolver.Agent enters the explicit branch:
	// the missing half (if any) is filled from the bucket and no
	// persistence happens.
	ExplicitTool  string
	ExplicitModel string

	// UI drives the interactive prompt branch. It must satisfy
	// picker.Selector (SelectTool + SelectModel).
	UI picker.Selector

	// Store, when non-nil, is reused for both reads and writes (test
	// injection). When nil, resolver opens `<cwd>/.j/settings` for the
	// duration of each individual operation and releases the lock
	// before returning so concurrent shells are not blocked.
	Store *store.Store

	// Stderr receives the "Choose your favourite:" prompt label and
	// any best-effort persistence warnings.
	Stderr io.Writer

	// Interactive is the resolved interactive flag (use
	// resolver.Interactive to compute it). Recorded alongside the
	// tool/model on the prompt branch so resume runs read a sensible
	// default.
	Interactive bool
}

// Agent walks the precedence chain (explicit → stored → prompt+persist)
// and returns the resolved agent + model.
func Agent(
	ctx context.Context, opts AgentOptions,
) (codingagents.Agent, string, error) {
	if opts.ExplicitTool != "" || opts.ExplicitModel != "" {
		// resolveExplicit never returns ErrNoStoredSelection when at
		// least one explicit flag is set: missing-half cases surface
		// as a typed error naming the supplied flag, so any non-nil
		// error here is terminal.
		return resolveExplicit(ctx, opts)
	}
	agent, model, err := agentFromStoreLazy(ctx, opts)
	if err == nil {
		return agent, model, nil
	}
	if !errors.Is(err, ErrNoStoredSelection) {
		return nil, "", err
	}
	if opts.Stderr != nil {
		uitheme.NormalFprintln(opts.Stderr, "J: Choose your favourite:")
	}
	agent, model, err = picker.PickAgent(ctx, opts.UI, opts.Agents)
	if err != nil {
		return nil, "", err
	}
	persistAgent(opts, agent.Name(), model)
	return agent, model, nil
}

// AgentFromStore reads the bucket's "tool" / "model" pair and returns
// the matching agent + model. Nil store, missing entries, or empty
// values all yield ErrNoStoredSelection.
func AgentFromStore(
	ctx context.Context, s *store.Store, bucket string,
	agents []codingagents.Agent,
) (codingagents.Agent, string, error) {
	if s == nil {
		return nil, "", ErrNoStoredSelection
	}
	values, err := readToolModel(s, bucket)
	if err != nil {
		return nil, "", err
	}
	tool := values["tool"]
	model := values["model"]
	if tool == "" || model == "" {
		return nil, "", ErrNoStoredSelection
	}
	agent, ok := lookupAgent(agents, tool)
	if !ok {
		return nil, "", fmt.Errorf("unknown tool %q", tool)
	}
	if err := agent.CheckLogin(ctx); err != nil {
		return nil, "", err
	}
	return agent, model, nil
}

// resolveAgent fills the missing half of the user-supplied --tool /
// --model pair from the bucket and runs CheckLogin. The store is
// never written. Both empty → ErrNoStoredSelection so the caller can
// fall back to AgentFromStore or Agent.
func resolveAgent(
	ctx context.Context, s *store.Store, bucket string,
	agents []codingagents.Agent, explicitTool, explicitModel string,
) (codingagents.Agent, string, error) {
	if explicitTool == "" && explicitModel == "" {
		return nil, "", ErrNoStoredSelection
	}
	tool, model := explicitTool, explicitModel
	if tool == "" || model == "" {
		stored, err := readToolModel(s, bucket)
		if err != nil {
			return nil, "", err
		}
		if tool == "" {
			tool = stored["tool"]
		}
		if model == "" {
			model = stored["model"]
		}
	}
	if tool == "" {
		return nil, "", fmt.Errorf(
			"resolver: --model given without stored tool in %s", bucket)
	}
	if model == "" {
		return nil, "", fmt.Errorf(
			"resolver: --tool given without stored model in %s", bucket)
	}
	agent, ok := lookupAgent(agents, tool)
	if !ok {
		return nil, "", fmt.Errorf("unknown tool %q", tool)
	}
	if err := agent.CheckLogin(ctx); err != nil {
		return nil, "", err
	}
	return agent, model, nil
}

func resolveExplicit(
	ctx context.Context, opts AgentOptions,
) (codingagents.Agent, string, error) {
	if opts.Store != nil {
		return resolveAgent(ctx, opts.Store, opts.Bucket, opts.Agents,
			opts.ExplicitTool, opts.ExplicitModel)
	}
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return resolveAgent(ctx, nil, opts.Bucket, opts.Agents,
			opts.ExplicitTool, opts.ExplicitModel)
	}
	defer func() { _ = s.Close() }()
	return resolveAgent(ctx, s, opts.Bucket, opts.Agents,
		opts.ExplicitTool, opts.ExplicitModel)
}

func agentFromStoreLazy(
	ctx context.Context, opts AgentOptions,
) (codingagents.Agent, string, error) {
	if opts.Store != nil {
		return AgentFromStore(ctx, opts.Store, opts.Bucket, opts.Agents)
	}
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return nil, "", ErrNoStoredSelection
	}
	defer func() { _ = s.Close() }()
	return AgentFromStore(ctx, s, opts.Bucket, opts.Agents)
}

func persistAgent(opts AgentOptions, tool, model string) {
	if opts.Store != nil {
		store.PersistAgentSelection(opts.Store, opts.Stderr, opts.Bucket,
			tool, model, opts.Interactive)
		return
	}
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return
	}
	defer func() { _ = s.Close() }()
	store.PersistAgentSelection(s, opts.Stderr, opts.Bucket,
		tool, model, opts.Interactive)
}

func readToolModel(s *store.Store, bucket string) (map[string]string, error) {
	values := map[string]string{}
	if s == nil {
		return values, nil
	}
	entries, err := s.List(bucket)
	if err != nil {
		return nil, fmt.Errorf("resolver: read %s: %w", bucket, err)
	}
	for _, kv := range entries {
		values[kv.Key] = kv.Value
	}
	return values, nil
}

// ResolveToolModel fills missing halves of the tool/model pair from the
// bucket. When both explicit values are already set the store is never
// opened. Errors from opening or reading the store are swallowed so the
// caller gets whatever could be resolved (the preflight
// EnsureAgentSelections call guarantees every bucket is populated).
func ResolveToolModel(
	explicitTool, explicitModel, bucket string, stderr io.Writer,
) (string, string) {
	tool, model := explicitTool, explicitModel
	if tool != "" && model != "" {
		return tool, model
	}
	s, ok := store.OpenSettings(stderr)
	if !ok {
		return tool, model
	}
	defer func() { _ = s.Close() }()
	entries, err := s.List(bucket)
	if err != nil {
		return tool, model
	}
	for _, kv := range entries {
		if tool == "" && kv.Key == "tool" {
			tool = kv.Value
		}
		if model == "" && kv.Key == "model" {
			model = kv.Value
		}
	}
	return tool, model
}

func lookupAgent(
	agents []codingagents.Agent, name string,
) (codingagents.Agent, bool) {
	for _, a := range agents {
		if a.Name() == name {
			return a, true
		}
	}
	return nil, false
}
