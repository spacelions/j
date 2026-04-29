// Package agentpick orchestrates the shared agent / model / login
// prompts used by both `j plan` and `j work`. Lifting the three-step
// flow out of those command packages prevents them from drifting apart
// and keeps each Run function focused on its own command-specific work.
//
// Each command keeps its own UI interface (plan and work intentionally
// have different shapes today and will diverge further as planner and
// coder grow apart). They both happen to satisfy the small Selector
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
	entries, err := s.List(bucket)
	if err != nil {
		return nil, "", fmt.Errorf("agentpick: read %s: %w", bucket, err)
	}
	values := make(map[string]string, len(entries))
	for _, kv := range entries {
		values[kv.Key] = kv.Value
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
