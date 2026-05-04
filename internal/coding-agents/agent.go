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
	//
	// The integer return is the OS process id of a fire-and-forget
	// background child when the backend opted to spawn one (headless
	// runs only); 0 means the call ran synchronously and there is
	// nothing for `j tasks` to reap later. A non-zero PID also means
	// the orchestrator MUST NOT read the output files yet: the child
	// is still writing them.
	Plan(ctx context.Context, req PlanRequest) (int, error)

	// Work runs the agent against an existing plan markdown file. The
	// agent edits files under the project directly; there is no single
	// output file the orchestrator can stat. Interactive selects the
	// agent's TUI; otherwise the agent runs headlessly against the same
	// prompt and exits when done.
	//
	// The integer return follows the same convention as Plan: a
	// non-zero PID means a detached background child was started
	// (headless only); 0 means synchronous completion.
	Work(ctx context.Context, req WorkRequest) (int, error)

	// Verify runs the agent against an existing requirements + plan
	// markdown pair, producing a verifier_plan.md and a verifier
	// findings markdown whose terminal line is exactly "VERDICT: PASS"
	// or "VERDICT: FAIL" inside the per-task workspace. On FAIL the
	// agent edits project files itself to address the findings before
	// exiting so the orchestrator can re-run the verifier turn after
	// a worker-resume fix loop. The agent is responsible for writing
	// req.VerifierPlanOutputPath and req.VerifierFindingsOutputPath
	// before exiting; the orchestrator reads the findings afterwards.
	//
	// The integer return follows the same convention as Plan / Work:
	// a non-zero PID means a detached background child was started
	// (headless only); 0 means synchronous completion.
	Verify(ctx context.Context, req VerifyRequest) (int, error)
}

// PlanRequest is the input to Agent.Plan.
//
// FromFilePath is the user-supplied requirement markdown path (from
// `j plan -f`); the agent reads it from disk (the backend prompt
// builders cite the path rather than embedding the body, so the
// orchestrator no longer pre-reads the markdown).
// RequirementsOutputPath and PlanOutputPath point inside
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
	Model                  string
	RequirementsOutputPath string
	PlanOutputPath         string
	Interactive            bool
	ResumeChatID           string
	// Resume, when true, asks the backend to use its resume-only
	// prompt template: skip the save-from-scratch suffix and tell
	// the previous session to inspect what was already done,
	// summarise it for the user, and continue only the outstanding
	// work. Independent of ResumeChatID, which still threads the
	// backend's session id.
	Resume bool
	// AgentLogPath is the absolute path the headless backend MUST
	// redirect stdout/stderr to when it spawns a fire-and-forget
	// background child. Empty for interactive runs and for backends
	// that do not implement background spawning. Backends that do
	// spawn must create or truncate the file with mode 0o644 inside
	// their detached spawn helper.
	AgentLogPath string
	// MustRead is the project-wide list of files every agent must
	// read before starting (sourced from the project.must_read
	// setting). Backends thread it into the first-run planner prompt
	// builder; resume runs ignore it. Empty preserves byte-identical
	// pre-must-read output.
	MustRead []string
}

// WorkRequest is the input to Agent.Work. There is no OutputPath:
// the worker edits files in place, so the orchestrator does not
// stat a single output file afterwards. The agent reads plan.md
// itself from PlanPath; the backend prompt builders cite the path
// rather than embedding the body, so the orchestrator no longer
// pre-reads the plan markdown.
//
// PlanPath is always an absolute path inside `<cwd>/.j/tasks/<id>/`
// (either an existing task directory for bbolt-sourced runs or a
// freshly-imported one for legacy `--from-file` runs).
//
// ResumeChatID, when set, is the value previously returned by
// Agent.NewResumeID. Agents that have no notion of resume ignore it.
type WorkRequest struct {
	PlanPath     string
	Model        string
	Interactive  bool
	ResumeChatID string
	// Resume, when true, asks the backend to use its resume-only
	// prompt template that tells the previous session to inspect
	// what was already done, summarise it for the user, and
	// continue only the outstanding work. Independent of
	// ResumeChatID.
	Resume bool
	// FixFindings, when true, asks the backend to use a fix-only
	// worker prompt that points the previous session at the
	// per-task verifier_findings.md (read by the agent itself) and
	// asks it to address every listed item without re-planning.
	// False preserves today's first-run / resume behaviour. Used
	// by `j verify`'s bounded fix loop after a verifier turn
	// returned VERDICT: FAIL.
	FixFindings bool
	// Worktree, when non-empty, is the bare git-worktree name the
	// worker should operate against. The backend threads it into the
	// prompt builders so the worker knows which worktree to `cd`
	// into (creating it via `git worktree add` if it does not yet
	// exist). Empty preserves the pre-R2 behaviour: the worker
	// operates against the main checkout. The value is NOT a path.
	Worktree string
	// AgentLogPath is the absolute path the headless backend MUST
	// redirect stdout/stderr to when it spawns a fire-and-forget
	// background child. Same contract as PlanRequest.AgentLogPath.
	AgentLogPath string
	// MustRead is the project-wide must-read list (see
	// PlanRequest.MustRead). Threaded into the first-run worker
	// prompt only; resume / fix runs ignore it.
	MustRead []string
}

// VerifyRequest is the input to Agent.Verify. The verifier reads
// the requirement and plan markdown itself from RequirementsPath
// and PlanPath (the backend prompt builders cite the paths rather
// than embedding bodies, so the orchestrator no longer pre-reads
// the markdown). The verifier writes its plan and findings
// markdown to the supplied output paths inside
// `<cwd>/.j/tasks/<id>/`; the orchestrator reads the findings
// afterwards to derive the VERDICT line.
//
// ResumeChatID, when set, is the value previously returned by
// Agent.NewResumeID; backends that have no notion of resume
// ignore it.
type VerifyRequest struct {
	RequirementsPath           string
	PlanPath                   string
	VerifierPlanOutputPath     string
	VerifierFindingsOutputPath string
	Model                      string
	Interactive                bool
	Resume                     bool
	ResumeChatID               string
	// Worktree, when non-empty, is the bare git-worktree name the
	// verifier should target. The backend threads it into the
	// verifier prompt so the agent can `git worktree list` the name
	// from the repository root and verify the code inside that
	// worktree. Empty preserves the pre-R3 behaviour: the verifier
	// inspects the main checkout. The value is NOT a path.
	Worktree string
	// AgentLogPath is the absolute path the headless backend MUST
	// redirect stdout/stderr to when it spawns a fire-and-forget
	// background child. Same contract as PlanRequest.AgentLogPath.
	AgentLogPath string
	// MustRead is the project-wide must-read list (see
	// PlanRequest.MustRead). Threaded into the first-run verifier
	// prompt only; resume runs ignore it.
	MustRead []string
}
