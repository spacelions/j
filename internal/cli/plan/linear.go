package plan

import (
	"context"

	"github.com/spacelions/j/internal/resolver"
)

// runLinear is the shared --from-linear / picker-Linear branch. It
// fetches the issue, builds the markdown body, picks the planner
// agent, and drives RunPlanFromBody so the body lands in
// requirements.md before the agent reads it. Errors from the auth /
// fetch path surface verbatim with the linear: prefix.
func runLinear(ctx context.Context, opts Options, identifier string) error {
	body, sourceLabel, err := resolver.FetchLinearBody(ctx, identifier)
	if err != nil {
		return err
	}
	agent, model, err := selectPlanner(ctx, opts)
	if err != nil {
		return err
	}
	return resolver.RunPlanFromBody(ctx, resolver.PlanMarkdownOptions{
		Stdout:            opts.Stdout,
		Stderr:            opts.Stderr,
		Agent:             agent,
		Model:             model,
		Interactive:       opts.Interactive,
		WaitForCompletion: opts.WaitForCompletion,
	}, body, sourceLabel, identifier)
}
