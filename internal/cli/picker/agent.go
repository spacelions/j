package picker

import (
	"context"
	"fmt"

	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// Selector is the slice of UI behaviour PickAgent needs. *Picker
// satisfies it via SelectTool / SelectModel; cli commands' narrow UI
// interfaces include the same two methods so their scripted fakes
// satisfy it too. resolver.AgentOptions.UI consumes this same
// interface for the prompt branch.
type Selector interface {
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
}

// SelectTool renders the agent picker over options. Title is generic
// ("Select tool") so the same widget serves planner / worker /
// verifier / tasks selections.
func (p *Picker) SelectTool(
	ctx context.Context, options []string,
) (string, error) {
	return p.choose(ctx, "Select tool", options)
}

// SelectModel renders the model picker over options. Same generic-
// title rationale as SelectTool; the upstream label / tool hint
// flows through the cli's prompt-before-this if it wants to clarify
// which role the user is configuring.
func (p *Picker) SelectModel(
	ctx context.Context, options []string,
) (string, error) {
	return p.choose(ctx, "Select model", options)
}

// PickAgent walks the shared three-step prompt:
//  1. ask which tool to use,
//  2. list that tool's models and ask which one,
//  3. verify the user is logged in to the chosen tool.
//
// CheckLogin runs last so the user is not asked to authenticate before
// they have committed to a tool / model. The non-UI variants
// (read-stored, explicit-flag) live in internal/resolver.
func PickAgent(
	ctx context.Context, ui Selector, agents []codingagents.Agent,
) (codingagents.Agent, string, error) {
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
