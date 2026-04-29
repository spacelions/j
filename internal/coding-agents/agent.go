// Package codingagents defines the Agent abstraction shared by every
// coding-agent backend (Cursor, Codex, Claude, ...). Concrete backends
// live in sibling sub-packages under internal/coding-agents/.
//
// The directory uses a dash (`coding-agents`) for readability, while the
// package identifier is a single lowercase word per Go convention.
package codingagents

import "context"

// Agent is a coding-agent backend. The plan and work packages orchestrate
// the flow generically: resolve a markdown source, list the agent's
// models, check login, then either Plan (writes the per-task
// requirements.md and plan.md inside .j/tasks/<id>/) or Work (executes
// a previously generated plan against the project root). New backends
// implement this interface.
type Agent interface {
	// Name is the short identifier shown in the tool picker (e.g. "cursor").
	Name() string

	// ListModels returns the models available for the signed-in user. An
	// error indicates the agent is misconfigured or unreachable.
	ListModels(ctx context.Context) ([]string, error)

	// CheckLogin verifies the user is signed in to the agent's CLI.
	CheckLogin(ctx context.Context) error

	// NewResumeID returns a fresh per-session token the agent can later
	// resume against. The orchestrator threads the value back into the
	// next call via PlanRequest.ResumeChatID or WorkRequest.ResumeChatID.
	// Agents that have no notion of session resume return ("", nil);
	// agents that do have one but failed to mint a fresh id (e.g. their
	// CLI is unreachable) return ("", err) and the caller decides
	// whether to warn-and-continue or abort.
	NewResumeID(ctx context.Context) (string, error)

	// Plan runs the agent for req. The agent is responsible for ensuring
	// req.RequirementsOutputPath and req.PlanOutputPath are written:
	// interactively (TUI session driven by the embedded save instruction
	// in the prompt) or headlessly (the same prompt suffix reaches the
	// agent which still saves the files via its tool use). The
	// orchestrator reads both files after this returns and reports the
	// outcome.
	Plan(ctx context.Context, req PlanRequest) error

	// Work runs the agent against an existing plan markdown file. The
	// agent edits files under the project directly; there is no single
	// output file the orchestrator can stat. Interactive selects the
	// agent's TUI; otherwise the agent runs headlessly against the same
	// prompt and exits when done.
	Work(ctx context.Context, req WorkRequest) error
}

// PlanRequest is the input to Agent.Plan. The caller pre-reads the
// requirement markdown body so the agent can choose how to embed or
// attach it without having to re-stat or re-read the file.
//
// FromFilePath is the user-supplied requirement markdown path (from
// `j plan -f`); RequirementsOutputPath and PlanOutputPath point inside
// `<cwd>/.j/tasks/<id>/` and are where the agent must write the
// (possibly refined) requirements summary and the produced plan,
// respectively, before exiting.
//
// ResumeChatID, when set, is the value previously returned by
// Agent.NewResumeID; the backend passes it to its CLI so the run
// continues that server-side thread. Agents that have no notion of
// resume ignore it.
type PlanRequest struct {
	FromFilePath           string
	Body                   string
	Model                  string
	RequirementsOutputPath string
	PlanOutputPath         string
	Interactive            bool
	ResumeChatID           string
}

// WorkRequest is the input to Agent.Work. The caller pre-reads the plan
// markdown body so the agent can choose how to embed or attach it
// without having to re-stat or re-read the file. There is no
// OutputPath: the coder edits files in place, so the orchestrator does
// not stat a single output file afterwards.
//
// PlanPath is always an absolute path inside `<cwd>/.j/tasks/<id>/`
// (either an existing task directory for bbolt-sourced runs or a
// freshly-imported one for legacy `--from-file` runs).
//
// ResumeChatID, when set, is the value previously returned by
// Agent.NewResumeID. Agents that have no notion of resume ignore it.
type WorkRequest struct {
	PlanPath     string
	Body         string
	Model        string
	Interactive  bool
	ResumeChatID string
}
