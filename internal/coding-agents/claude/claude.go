// Package claude implements the codingagents.Agent backed by the local
// claude CLI (Anthropic's Claude Code). It is the second concrete
// backend alongside internal/coding-agents/cursor and is wired into
// the same picker shown by `j plan / j work / j verify`.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/util/run"
)

// Binary is the claude executable name.
const Binary = "claude"

const (
	argPrint                      = "--print"
	argOutputFormat               = "--output-format"
	argOutputFormatText           = "text"
	argDangerouslySkipPermissions = "--dangerously-skip-permissions"
	argModel                      = "--model"
)

// defaultModels is the picker list shown for `j plan / j work / j verify`.
// Claude has no `--list-models` equivalent so we surface the stable set
// of latest-model aliases the CLI accepts via `--model`. Users who want
// to pin a specific full id (e.g. `claude-opus-4-7`) can record it via
// `j settings set model <id>`.
//nolint:goconst // "sonnet" count inflated by test files; 1 production use
var defaultModels = []string{"opus", "sonnet", "haiku"}

// Agent is a Claude-backed worker. It is stateless: every method shells
// out to the real claude binary on PATH via the run package's
// package-level helpers. Tests drive it with a stub binary on PATH
// rather than an injected runner (see AGENTS.md "no test seams" rule).
type Agent struct{}

var _ codingagents.Agent = (*Agent)(nil)

// New returns a Claude agent that shells out to the claude CLI.
func New() *Agent { return &Agent{} }

// Name implements codingagents.Agent.
func (*Agent) Name() string { return "claude" }

// NewResumeID mints a fresh UUID locally. Claude has no `create-chat`
// command (sessions are minted server-side on the first call to claude
// when `--session-id <uuid>` is supplied), so we generate the id here
// and let Plan/Work/Verify decide whether to register it via
// `--session-id` (first run) or thread it through `--resume <id>`
// (resume run).
func (*Agent) NewResumeID(_ context.Context) (string, error) {
	return uuid.New().String(), nil
}

// ListModels returns the static latest-model alias set the picker
// shows. It does not shell out: the claude CLI has no `--list-models`
// command and the set is small and stable.
func (*Agent) ListModels(_ context.Context) ([]string, error) {
	out := make([]string, len(defaultModels))
	copy(out, defaultModels)
	return out, nil
}

// CheckLogin verifies the user is signed in to claude. The CLI's
// `auth status` subcommand prints JSON by default with a `loggedIn`
// boolean field. A non-zero exit, unparseable output, or
// `loggedIn=false` all surface a remediation hint pointing at
// `claude auth login`.
func (*Agent) CheckLogin(ctx context.Context) error {
	out, err := run.Output(ctx, Binary, "auth", "status")
	if err != nil {
		return fmt.Errorf(
			"claude auth status failed: %w "+
				"(run 'claude auth login')",
			err,
		)
	}
	var status struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if jsonErr := json.Unmarshal(
		[]byte(strings.TrimSpace(out)), &status,
	); jsonErr != nil {
		return errors.New(
			"claude reports not logged in; run 'claude auth login'",
		)
	}
	if !status.LoggedIn {
		return errors.New(
			"claude reports not logged in; run 'claude auth login'",
		)
	}
	return nil
}

// Plan runs claude against req. Two flavours are supported:
//
//   - Interactive (req.Interactive == true): launch claude's TUI with
//     `--permission-mode plan` and ask claude to save both files
//     before exiting via a suffix on the prompt. The TUI is allowed
//     to leave plan mode and approve writes manually.
//   - Headless (req.Interactive == false): `--print --output-format text
//     --dangerously-skip-permissions --model <m> -- <prompt>`. We drop
//     `--permission-mode plan` here on purpose: plan mode is read-only
//     and would forbid every write tool call, blocking the prompt's
//     "save requirements/plan" instructions.
//     `--dangerously-skip-permissions` auto-approves tool calls and
//     skips the workspace-trust prompt; combined they make the
//     headless run actually complete without stalling. The literal
//     `--` separator pins the prompt as a positional so a leading
//     `-`/`--` line in the user's spec body is not parsed as a flag
//     (which would otherwise make the CLI bail with an "unknown
//     option" error and never start the session).
//
// cmd.Dir is set to the per-task workspace dir so claude's CLAUDE.md
// auto-discovery and tool scope land where the user expects.
func (a *Agent) Plan(
	ctx context.Context, req codingagents.PlanRequest,
) (int, error) {
	workspace := codingagents.DefaultWorkspace(req.FromFilePath)
	prompt := buildPlanPrompt(req)

	if req.Interactive {
		args := append(
			sessionArgs(req.ResumeChatID, req.Resume),
			"--permission-mode", "plan",
			argModel, req.Model, prompt,
		)
		if err := run.RunIn(
			ctx, workspace, Binary, args...,
		); err != nil {
			return 0, fmt.Errorf("claude: %w", err)
		}
		return 0, nil
	}

	hargs := append(
		sessionArgs(req.ResumeChatID, req.Resume),
		headlessArgs(req.Model, prompt)...,
	)
	pid, err := run.SpawnIn(
		ctx, workspace, req.AgentLogPath, Binary, hargs...,
	)
	if err != nil {
		return 0, fmt.Errorf("claude: %w", err)
	}
	return pid, nil
}

// headlessArgs returns the argv tail used by Plan / Work / Verify
// in headless mode: `--print --output-format text
// --dangerously-skip-permissions --model <m> -- <prompt>`. The
// literal `--` pins the prompt as a positional so a leading `-` /
// `--` line in the user's spec body is not mis-parsed as a flag.
func headlessArgs(model, prompt string) []string {
	return []string{
		argPrint, argOutputFormat, argOutputFormatText,
		argDangerouslySkipPermissions,
		argModel, model, "--", prompt,
	}
}

// Work runs claude against a previously generated plan markdown. The
// agent edits files directly so we do not pass `--permission-mode
// plan`. Two flavours mirror Plan:
//
//   - Interactive: launch claude's TUI with the worker prompt as the
//     initial user message; the user drives the session.
//   - Headless: `--print --output-format text --dangerously-skip-permissions
//     --model <m>`, fire-and-forget. claude's stdout/stderr are
//     redirected to req.AgentLogPath via run.SpawnIn and the spawned
//     PID is returned so `j work` can record it for later reaping.
func (a *Agent) Work(
	ctx context.Context, req codingagents.WorkRequest,
) (int, error) {
	workspace := codingagents.DefaultWorkspace(req.PlanPath)
	prompt := buildWorkPrompt(req)

	if req.Interactive {
		args := append(
			sessionArgs(req.ResumeChatID, req.Resume),
			argModel, req.Model, prompt,
		)
		if err := run.RunIn(
			ctx, workspace, Binary, args...,
		); err != nil {
			return 0, fmt.Errorf("claude: %w", err)
		}
		return 0, nil
	}

	pargs := append(
		sessionArgs(req.ResumeChatID, req.Resume),
		headlessArgs(req.Model, prompt)...,
	)
	pid, err := run.SpawnIn(
		ctx, workspace, req.AgentLogPath, Binary, pargs...,
	)
	if err != nil {
		return 0, fmt.Errorf("claude: %w", err)
	}
	return pid, nil
}

// Verify runs claude against the requirements + plan pair. Mirrors
// cursor's Verify: cmd.Dir is the project root (so the verifier can
// `git worktree list` from there) rather than the per-task dir, and
// `--permission-mode plan` is intentionally absent because the
// verifier needs to write verifier_plan.md / verifier_findings.md
// (and edit project files on FAIL).
func (a *Agent) Verify(
	ctx context.Context, req codingagents.VerifyRequest,
) (int, error) {
	workspace := codingagents.ProjectRootWorkspace()
	prompt := buildVerifyPrompt(req)

	if req.Interactive {
		args := append(
			sessionArgs(req.ResumeChatID, req.Resume),
			argModel, req.Model, prompt,
		)
		if err := run.RunIn(
			ctx, workspace, Binary, args...,
		); err != nil {
			return 0, fmt.Errorf("claude: %w", err)
		}
		return 0, nil
	}

	pargs := append(
		sessionArgs(req.ResumeChatID, req.Resume),
		headlessArgs(req.Model, prompt)...,
	)
	pid, err := run.SpawnIn(
		ctx, workspace, req.AgentLogPath, Binary, pargs...,
	)
	if err != nil {
		return 0, fmt.Errorf("claude: %w", err)
	}
	return pid, nil
}

// sessionArgs returns the leading argv segment that threads the
// orchestrator's session id into claude. Empty id yields a nil slice
// (claude picks its own session id). When resume is true the id is
// passed via `--resume`; otherwise via `--session-id` so the new
// server-side session is bound to that uuid for later resume.
func sessionArgs(id string, resume bool) []string {
	if id == "" {
		return nil
	}
	if resume {
		return []string{"--resume", id}
	}
	return []string{"--session-id", id}
}
