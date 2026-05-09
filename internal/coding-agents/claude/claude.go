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

	"github.com/spacelions/j/internal/agents/prompts"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/util/run"
)

// Binary is the claude executable name.
const Binary = "claude"

const (
	argPrint                      = "--print"
	argOutputFormat               = "--output-format"
	argOutputFormatStreamJSON     = "stream-json"
	argVerbose                    = "--verbose"
	argDangerouslySkipPermissions = "--dangerously-skip-permissions"
	argModel                      = "--model"
)

// defaultModels is the picker list shown for `j plan / j work / j verify`.
// Claude has no `--list-models` equivalent so we surface the stable set
// of latest-model aliases the CLI accepts via `--model`. Users who want
// to pin a specific full id (e.g. `claude-opus-4-7`) can record it via
// `j settings set model <id>`.
//
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
//   - Headless (req.Interactive == false): `--print --output-format
//     stream-json --verbose --include-partial-messages
//     --dangerously-skip-permissions --model <m> -- <prompt>`. We
//     drop `--permission-mode plan` here on purpose: plan mode is
//     read-only and would forbid every write tool call, blocking
//     the prompt's "save requirements/plan" instructions.
//     `--dangerously-skip-permissions` auto-approves tool calls and
//     skips the workspace-trust prompt; combined they make the
//     headless run complete without stalling. The stream-json,
//     verbose, and include-partial-messages flags together make
//     claude emit one JSON event per turn (system init, assistant
//     content, tool_use, tool_result, final result) instead of only
//     the final assistant text — those raw event lines land verbatim
//     in agent.log via run.SpawnIn so a tailer can `jq` over them
//     to surface thinking / tool calls. The literal `--` separator
//     pins the prompt as a positional so a leading `-` / `--` line
//     in the user's spec body is not parsed as a flag.
//
// cmd.Dir is set to the per-task workspace dir so claude's CLAUDE.md
// auto-discovery and tool scope land where the user expects.
func (a *Agent) Plan(
	ctx context.Context, req codingagents.PlanRequest,
) (int, error) {
	workspace := codingagents.DefaultWorkspace(req.FromFilePath)
	prompt := prompts.PlanPrompt(req)

	iargs := phaseArgs(
		req.ResumeChatID, req.Resume,
		"--permission-mode", "plan", argModel, req.Model, prompt,
	)
	hargs := headlessPhaseArgs(req.ResumeChatID, req.Resume, req.Model, prompt)
	return a.runPhase(ctx, phaseRun{
		interactive:     req.Interactive,
		workspace:       workspace,
		agentLogPath:    req.AgentLogPath,
		interactiveArgs: iargs,
		headlessArgs:    hargs,
	})
}

// headlessArgs returns the argv tail used by Plan / Work / Verify in
// headless mode: `--print --output-format stream-json --verbose
// --dangerously-skip-permissions --model <m> -- <prompt>`.
// `--verbose` is required by the claude CLI whenever stream-json is
// selected with `--print` (the help text spells out the dependency).
// `--include-partial-messages` was dropped in SPA-73: the run helper
// now parses the per-turn aggregated events (system init, assistant
// content blocks, tool_use, tool_result, final result) and renders
// them as agentlog-style marker lines via Agent.FormatLog, so the
// 30–200 partial-delta lines per turn are pure noise we can skip.
// The literal `--` pins the prompt as a positional so a leading `-`
// / `--` line in the user's spec body is not mis-parsed as a flag.
func headlessArgs(model, prompt string) []string {
	return []string{
		argPrint,
		argOutputFormat, argOutputFormatStreamJSON,
		argVerbose,
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
//   - Headless: same flag set as Plan's headless branch
//     (stream-json, verbose, include-partial-messages,
//     dangerously-skip-permissions), fire-and-forget. claude's
//     stdout / stderr are redirected to
//     req.AgentLogPath via run.SpawnIn — the raw JSON event lines
//     interleave with the existing lifecycle markers so the per-task
//     agent.log captures every assistant content block / tool call /
//     tool result, not just the final assistant text. The spawned
//     PID is returned so `j work` can record it for later reaping.
func (a *Agent) Work(
	ctx context.Context, req codingagents.WorkRequest,
) (int, error) {
	workspace := codingagents.DefaultWorkspace(req.PlanPath)
	prompt := prompts.WorkPrompt(req)

	iargs := phaseArgs(
		req.ResumeChatID, req.Resume,
		argModel, req.Model, prompt,
	)
	hargs := headlessPhaseArgs(req.ResumeChatID, req.Resume, req.Model, prompt)
	return a.runPhase(ctx, phaseRun{
		interactive:     req.Interactive,
		workspace:       workspace,
		agentLogPath:    req.AgentLogPath,
		interactiveArgs: iargs,
		headlessArgs:    hargs,
	})
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
	prompt := prompts.VerifyPrompt(req)

	iargs := phaseArgs(
		req.ResumeChatID, req.Resume,
		argModel, req.Model, prompt,
	)
	hargs := headlessPhaseArgs(req.ResumeChatID, req.Resume, req.Model, prompt)
	return a.runPhase(ctx, phaseRun{
		interactive:     req.Interactive,
		workspace:       workspace,
		agentLogPath:    req.AgentLogPath,
		interactiveArgs: iargs,
		headlessArgs:    hargs,
	})
}

type phaseRun struct {
	interactive     bool
	workspace       string
	agentLogPath    string
	interactiveArgs []string
	headlessArgs    []string
}

func (a *Agent) runPhase(ctx context.Context, phase phaseRun) (int, error) {
	if phase.interactive {
		if err := run.RunIn(
			ctx, phase.workspace, Binary, phase.interactiveArgs...,
		); err != nil {
			return 0, fmt.Errorf("claude: %w", err)
		}
		return 0, nil
	}
	pid, err := run.SpawnFormattedIn(
		ctx, phase.workspace, phase.agentLogPath,
		a.FormatLog, Binary, phase.headlessArgs...,
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

func phaseArgs(id string, resume bool, args ...string) []string {
	return append(sessionArgs(id, resume), args...)
}

func headlessPhaseArgs(id string, resume bool, model, prompt string) []string {
	return append(sessionArgs(id, resume), headlessArgs(model, prompt)...)
}
