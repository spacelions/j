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
	"fmt"

	codingagents "github.com/spacelions/j/internal/coding-agents"
)

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
