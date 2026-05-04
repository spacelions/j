package picker

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// ErrNoStoredSelection is returned by AgentFromStore when the supplied
// store is nil or the bucket does not yet hold both a "tool" and a
// "model" entry. Callers use errors.Is to detect this sentinel and
// fall back to the interactive PickAgent flow on first runs.
var ErrNoStoredSelection = errors.New("picker: no stored selection")

// Selector is the slice of UI behaviour PickAgent needs. *Picker
// satisfies it via SelectTool / SelectModel; cli commands' narrow UI
// interfaces include the same two methods so their scripted fakes
// satisfy it too.
type Selector interface {
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
}

// SelectTool renders the agent picker over options. Title is generic
// ("Select tool") so the same widget serves planner / worker /
// verifier / tasks selections.
func (p *Picker) SelectTool(ctx context.Context, options []string) (string, error) {
	return p.choose(ctx, "Select tool", options)
}

// SelectModel renders the model picker over options. Same generic-
// title rationale as SelectTool; the upstream label / tool hint
// flows through the cli's prompt-before-this if it wants to clarify
// which role the user is configuring.
func (p *Picker) SelectModel(ctx context.Context, options []string) (string, error) {
	return p.choose(ctx, "Select model", options)
}

// PickAgent walks the shared three-step flow:
//  1. ask which tool to use,
//  2. list that tool's models and ask which one,
//  3. verify the user is logged in to the chosen tool.
//
// CheckLogin runs last so the user is not asked to authenticate before
// they have committed to a tool / model.
func PickAgent(ctx context.Context, ui Selector, agents []codingagents.Agent) (codingagents.Agent, string, error) {
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name()
	}
	chosen, err := ui.SelectTool(ctx, names)
	if err != nil {
		return nil, "", err
	}
	agent, ok := lookupAgent(agents, chosen)
	if !ok {
		return nil, "", fmt.Errorf("unknown tool %q", chosen)
	}

	models, err := agent.ListModels(ctx)
	if err != nil {
		return nil, "", err
	}
	model, err := ui.SelectModel(ctx, models)
	if err != nil {
		return nil, "", err
	}

	if err := agent.CheckLogin(ctx); err != nil {
		return nil, "", err
	}
	return agent, model, nil
}

// ResolveAgent handles the explicit-with-stored-fallback path used by
// the --tool / --model one-off override flags on `j plan|work|verify`.
// It is the read-only counterpart to PickAgent: when the caller
// supplies at least one of explicitTool / explicitModel, the missing
// half (if any) is filled from the bucket and CheckLogin runs against
// the resolved agent. The store is never written.
//
// Behaviour:
//   - both empty → ErrNoStoredSelection (caller falls back to
//     AgentFromStore or PickAgent).
//   - one empty + bucket has the missing key → fill from bucket.
//   - one empty + bucket missing → wrapped error naming the supplied
//     flag and the bucket so the user can run `j settings reset` or
//     pass the missing flag.
//   - unknown tool name → "unknown tool %q".
//   - CheckLogin failure propagates verbatim.
func ResolveAgent(ctx context.Context, s *store.Store, bucket string, agents []codingagents.Agent, explicitTool, explicitModel string) (codingagents.Agent, string, error) {
	if explicitTool == "" && explicitModel == "" {
		return nil, "", ErrNoStoredSelection
	}
	tool, model := explicitTool, explicitModel
	if tool == "" || model == "" {
		stored, err := storedToolModel(s, bucket)
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
		return nil, "", fmt.Errorf("picker: --model given without stored tool in %s", bucket)
	}
	if model == "" {
		return nil, "", fmt.Errorf("picker: --tool given without stored model in %s", bucket)
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

// AgentFromStore reuses a previously-recorded tool/model selection
// from the bbolt settings store instead of prompting the user. It
// reads the "tool" and "model" keys from the supplied bucket, looks
// the agent up by name, and runs CheckLogin so authentication
// failures surface here exactly as they do in PickAgent.
//
// The store is treated as read-only: callers that record a selection
// do so on the prompted path only, so this helper never re-Puts the
// values it reads. A nil store, a missing tool entry, or a missing
// model entry all yield ErrNoStoredSelection so the caller can
// transparently fall back to PickAgent on a first run.
func AgentFromStore(ctx context.Context, s *store.Store, bucket string, agents []codingagents.Agent) (codingagents.Agent, string, error) {
	if s == nil {
		return nil, "", ErrNoStoredSelection
	}
	values, err := storedToolModel(s, bucket)
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

// StoredInteractive returns the parsed `interactive` value recorded
// in bucket, and a boolean indicating whether a usable value was
// found. A nil store, missing key, empty value, or unparseable value
// all yield (false, false). Treated as advisory: callers fall back
// to opts.Interactive when ok is false.
func StoredInteractive(s *store.Store, bucket string) (bool, bool) {
	if s == nil {
		return false, false
	}
	v, ok, err := s.Get(bucket, "interactive")
	if err != nil || !ok || v == "" {
		return false, false
	}
	parsed, perr := strconv.ParseBool(v)
	if perr != nil {
		return false, false
	}
	return parsed, true
}

// storedToolModel reads the bucket's "tool" and "model" entries into
// a map for the partial-flag fallback in ResolveAgent. A nil store
// returns an empty map (no fallback available); a real read error is
// wrapped so the caller can surface a clear failure instead of
// silently falling through to "missing half" errors.
func storedToolModel(s *store.Store, bucket string) (map[string]string, error) {
	values := map[string]string{}
	if s == nil {
		return values, nil
	}
	entries, err := s.List(bucket)
	if err != nil {
		return nil, fmt.Errorf("picker: read %s: %w", bucket, err)
	}
	for _, kv := range entries {
		values[kv.Key] = kv.Value
	}
	return values, nil
}

// lookupAgent returns the first agent whose Name matches. The caller
// is expected to pass a name produced by the same UI list, so a miss
// here means the UI returned something off-list (real huh menus
// can't, but scripted fakes can — the caller surfaces this as
// "unknown tool").
func lookupAgent(agents []codingagents.Agent, name string) (codingagents.Agent, bool) {
	for _, a := range agents {
		if a.Name() == name {
			return a, true
		}
	}
	return nil, false
}
