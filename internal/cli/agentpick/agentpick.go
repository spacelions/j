// Package agentpick orchestrates the shared agent / model / login
// prompts used by both `j plan` and `j work`. Lifting the three-step
// flow out of those command packages prevents them from drifting apart
// and keeps each Run function focused on its own command-specific work.
//
// Each command keeps its own UI interface (plan and work intentionally
// have different shapes today and will diverge further as planner and
// worker grow apart). They both happen to satisfy the small Selector
// surface declared here, so callers pass their UI straight in.
package agentpick

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// ErrNoStoredSelection is returned by FromStore when the supplied
// store is nil or the bucket does not yet hold both a "tool" and a
// "model" entry. Callers use errors.Is to detect this sentinel and
// fall back to the interactive Pick flow on first runs.
var ErrNoStoredSelection = errors.New("agentpick: no stored selection")

// Selector is the slice of UI behavior that Pick needs. Defining it
// locally avoids importing either command package and keeps the
// dependency direction CLI -> agentpick (not the reverse).
type Selector interface {
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
}

// Pick walks the shared three-step flow:
//  1. ask which tool to use,
//  2. list that tool's models and ask which one,
//  3. verify the user is logged in to the chosen tool.
//
// It returns the chosen agent, the chosen model, and any error from
// the UI or the agent. CheckLogin runs last so the user is not asked
// to authenticate before they have committed to a tool / model.
func Pick(ctx context.Context, ui Selector, agents []codingagents.Agent) (codingagents.Agent, string, error) {
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name()
	}
	chosen, err := ui.SelectTool(ctx, names)
	if err != nil {
		return nil, "", err
	}
	agent, ok := lookup(agents, chosen)
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

// Resolve handles the explicit-with-stored-fallback path used by the
// new --tool / --model one-off override flags on `j plan|work|verify`.
// It is the read-only counterpart to Pick: when the caller supplies at
// least one of explicitTool / explicitModel, the missing half (if any)
// is filled from the bucket and CheckLogin runs against the resolved
// agent. The store is never written.
//
// Behavior:
//   - both empty → ErrNoStoredSelection (caller falls back to FromStore
//     or Pick).
//   - one empty + bucket has the missing key → fill from bucket.
//   - one empty + bucket missing → wrapped error naming the supplied
//     flag and the bucket so the user can run `j settings reset` or
//     pass the missing flag.
//   - unknown tool name → "unknown tool %q" (same shape as Pick /
//     FromStore so error handling stays uniform across the three
//     selection paths).
//   - CheckLogin failure propagates verbatim.
func Resolve(ctx context.Context, s *store.Store, bucket string, agents []codingagents.Agent, explicitTool, explicitModel string) (codingagents.Agent, string, error) {
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
		return nil, "", fmt.Errorf("agentpick: --model given without stored tool in %s", bucket)
	}
	if model == "" {
		return nil, "", fmt.Errorf("agentpick: --tool given without stored model in %s", bucket)
	}
	agent, ok := lookup(agents, tool)
	if !ok {
		return nil, "", fmt.Errorf("unknown tool %q", tool)
	}
	if err := agent.CheckLogin(ctx); err != nil {
		return nil, "", err
	}
	return agent, model, nil
}

// storedToolModel reads the bucket's "tool" and "model" entries into
// a map for the partial-flag fallback in Resolve. A nil store returns
// an empty map (no fallback available); a real read error is wrapped
// like FromStore so the caller can surface a clear failure instead of
// silently falling through to "missing half" errors.
func storedToolModel(s *store.Store, bucket string) (map[string]string, error) {
	values := map[string]string{}
	if s == nil {
		return values, nil
	}
	entries, err := s.List(bucket)
	if err != nil {
		return nil, fmt.Errorf("agentpick: read %s: %w", bucket, err)
	}
	for _, kv := range entries {
		values[kv.Key] = kv.Value
	}
	return values, nil
}

// FromStore reuses a previously-recorded tool/model selection from
// the bbolt settings store instead of prompting the user. It reads
// the "tool" and "model" keys from the supplied bucket, looks the
// agent up by name, and runs CheckLogin so authentication failures
// surface here exactly as they do in Pick.
//
// The store is treated as read-only: callers that record a
// selection do so on the prompted path only, so this helper never
// re-Puts the values it reads. A nil store, a missing tool entry,
// or a missing model entry all yield ErrNoStoredSelection so the
// caller can transparently fall back to Pick on a first run.
func FromStore(ctx context.Context, s *store.Store, bucket string, agents []codingagents.Agent) (codingagents.Agent, string, error) {
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
	agent, ok := lookup(agents, tool)
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

// lookup returns the first agent whose Name matches. The caller is
// expected to pass a name produced by the same UI list, so a miss
// here means the UI returned something off-list (real huh menus
// can't, but scripted fakes can — the caller surfaces this as
// "unknown tool").
func lookup(agents []codingagents.Agent, name string) (codingagents.Agent, bool) {
	for _, a := range agents {
		if a.Name() == name {
			return a, true
		}
	}
	return nil, false
}
