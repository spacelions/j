// Package codingagents defines the Agent abstraction shared by every
// coding-agent backend (Cursor, Codex, Claude, ...). Concrete backends
// live in sibling sub-packages under internal/coding-agents/.
//
// The directory uses a dash (`coding-agents`) for readability, while the
// package identifier is a single lowercase word per Go convention.
package codingagents

import "context"

// Agent is a coding-agent backend. The plan and work packages orchestrate
// the flow generically: resolve a markdown target, list the agent's
// models, check login, then either Plan (writes <stem>.plan.md) or Work
// (executes a previously generated plan against the plan's directory).
// New backends implement this interface.
type Agent interface {
	// Name is the short identifier shown in the tool picker (e.g. "cursor").
	Name() string

	// ListModels returns the models available for the signed-in user. An
	// error indicates the agent is misconfigured or unreachable.
	ListModels(ctx context.Context) ([]string, error)

	// CheckLogin verifies the user is signed in to the agent's CLI.
	CheckLogin(ctx context.Context) error

	// Plan runs the agent for req. The agent is responsible for ensuring
	// req.OutputPath is written: interactively (TUI session driven by the
	// embedded save instruction in the prompt) or headlessly (capturing
	// the agent's stdout and writing the file directly). The orchestrator
	// stats OutputPath after this returns and reports the outcome.
	Plan(ctx context.Context, req PlanRequest) error

	// Work runs the agent against an existing plan markdown file. The
	// agent edits files in the plan's directory directly; there is no
	// single output file the orchestrator can stat. Interactive selects
	// the agent's TUI; otherwise the agent runs headlessly against the
	// same prompt and exits when done.
	Work(ctx context.Context, req WorkRequest) error
}

// PlanRequest is the input to Agent.Plan. The caller pre-reads the
// markdown body so the agent can choose how to embed or attach it
// without having to re-stat or re-read the file.
//
// An empty TargetPath signals a "scratch" session: no markdown body,
// no expected output file, OutputPath is also empty, and the agent
// should hand its plan-mode TUI to the user as-is. A non-empty
// TargetPath always pairs with a non-empty OutputPath and the agent
// is expected to leave OutputPath behind on success.
//
// ResumeChatID, when set, is the Cursor agent chat session id: the
// backend passes it to `cursor-agent --resume` so the run continues
// that server-side thread. Other agents ignore it.
type PlanRequest struct {
	TargetPath   string
	Body         string
	Model        string
	OutputPath   string
	Interactive  bool
	ResumeChatID string
}

// WorkRequest is the input to Agent.Work. The caller pre-reads the plan
// markdown body so the agent can choose how to embed or attach it
// without having to re-stat or re-read the file. Unlike PlanRequest
// there is no OutputPath: the coder edits files in the plan's
// directory directly, so the orchestrator does not stat a single
// output file afterwards.
//
// ResumeChatID, when set, is the Cursor agent chat session id for
// `cursor-agent --resume`. Other agents ignore it.
type WorkRequest struct {
	PlanPath     string
	Body         string
	Model        string
	Interactive  bool
	ResumeChatID string
}
