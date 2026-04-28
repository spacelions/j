// Package codingagents defines the Agent abstraction shared by every
// coding-agent backend (Cursor, Codex, Claude, ...). Concrete backends
// live in sibling sub-packages under internal/coding-agents/.
//
// The directory uses a dash (`coding-agents`) for readability, while the
// package identifier is a single lowercase word per Go convention.
package codingagents

import "context"

// Agent is a planning backend. The plan package orchestrates the flow generically:
// resolve a markdown target, list the agent's models, check login, run the
// plan, and write plan.md beside the target. New backends implement this
// interface.
type Agent interface {
	// Name is the short identifier shown in the tool picker (e.g. "cursor").
	Name() string

	// ListModels returns the models available for the signed-in user. An
	// error indicates the agent is misconfigured or unreachable.
	ListModels(ctx context.Context) ([]string, error)

	// CheckLogin verifies the user is signed in to the agent's CLI.
	CheckLogin(ctx context.Context) error

	// Plan asks the agent to produce an ordered implementation plan for
	// the markdown task. The returned string is written to plan.md by the
	// caller; the agent should not attempt to write files itself.
	Plan(ctx context.Context, req PlanRequest) (string, error)
}

// PlanRequest is the input to Agent.Plan. The caller pre-reads the
// markdown body so the agent can choose how to embed or attach it without
// having to re-stat or re-read the file.
type PlanRequest struct {
	TargetPath string
	Body       string
	Model      string
}
