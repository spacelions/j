package picker

import "context"

// SelectTool renders the agent picker over options. Title is generic
// ("Select tool") so the same widget serves planner / worker /
// verifier / tasks selections. The signature matches
// agentpick.Selector so *Picker can be passed to agentpick.Pick
// directly.
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
