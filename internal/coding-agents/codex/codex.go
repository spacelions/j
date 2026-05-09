// Package codex implements the codingagents.Agent backed by the local
// codex CLI (OpenAI's Codex CLI). It is the fourth concrete backend
// alongside internal/coding-agents/cursor, internal/coding-agents/claude
// and internal/coding-agents/deepseek and is wired into the same picker
// shown by `j plan / j work / j verify`.
//
// CLI surface decisions (derived from `codex --help`, `codex exec --help`,
// `codex exec resume --help`, `codex login --help` on codex-cli 0.130.0):
//
//   - Binary is `codex` (`codex --version` prints `codex-cli <ver>`).
//   - Headless entrypoint is `codex exec [PROMPT]` (or
//     `codex exec resume <id> [PROMPT]` for resume runs). The prompt is
//     a positional argument; we pin it behind a literal `--` to guard
//     against a leading `-` line in the user's spec body being parsed
//     as a flag (mirrors the claude / deepseek backends).
//   - Login probe is `codex login status`: exit code 0 means logged in,
//     non-zero means logged out (the CLI also prints "Not logged in"
//     to stdout, but we rely on the exit code only — the cleanest
//     non-zero-exit-on-logged-out signal across the four backends).
//   - There is no `--list-models` command, so defaultModels is a static
//     canonical alias slice mirroring claude / deepseek. Users pin a
//     specific id via `j settings set <bucket>.model=<id>`.
//   - There is no pre-run `--session-id <uuid>` binding flag (codex
//     mints the thread id server-side and writes it to the rollout
//     file as the first session_meta event). NewResumeID therefore
//     always returns ("", nil) and the post-run id is recovered by
//     CaptureResumeID (capture.go) which scans
//     `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*.jsonl` (falling back
//     to `~/.codex/sessions`) — same pattern as deepseek.
//   - Codex's default `exec` output is multi-line human text rather
//     than stream-json, so FormatLog is the identity transform — the
//     bytes the child wrote already read like the rest of agent.log.
//     `--json` is intentionally not requested: an identity formatter
//     over JSONL would dump raw envelopes that need their own renderer
//     (claude precedent) and the human text trace is already readable.
package codex

import (
	"context"
	"fmt"

	"github.com/spacelions/j/internal/agents/prompts"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/util/run"
)

// Binary is the codex executable name.
const Binary = "codex"

const (
	argExec    = "exec"
	argResume  = "resume"
	argModel   = "-m"
	argSkipGit = "--skip-git-repo-check"
	argBypass  = "--dangerously-bypass-approvals-and-sandbox"
)

// defaultModels is the picker list shown for `j plan / j work / j verify`.
// codex has no `--list-models` command and ships a stable canonical
// alias the CLI accepts via `-m`. Users who want a different specific
// id can pin it via `j settings set <bucket>.model=<id>` (mirrors the
// claude / deepseek precedent).
//
//nolint:goconst // "gpt-5.5" count inflated by test fixtures
var defaultModels = []string{"gpt-5.5"}

// Agent is a codex-backed worker. It is stateless: every method shells
// out to the real codex binary on PATH via the run package's
// package-level helpers. Tests drive it with a stub binary on PATH
// rather than an injected runner (see AGENTS.md "no test seams" rule).
type Agent struct{}

var (
	_ codingagents.Agent            = (*Agent)(nil)
	_ codingagents.ResumeIDCapturer = (*Agent)(nil)
)

// New returns a codex agent that shells out to the codex CLI.
func New() *Agent { return &Agent{} }

// Name implements codingagents.Agent.
func (*Agent) Name() string { return "codex" }

// NewResumeID always returns ("", nil): codex has no pre-run
// `--session-id`-style binding flag, so there is nothing to mint
// pre-run. The thread id is captured post-run by CaptureResumeID
// (capture.go) and threaded into later runs via
// PlanRequest/WorkRequest/VerifyRequest.ResumeChatID.
func (*Agent) NewResumeID(_ context.Context) (string, error) {
	return "", nil
}

// ListModels returns the static canonical alias set the picker shows.
// It does not shell out: codex has no list command and the set is
// small and stable. The returned slice is a fresh copy so callers
// cannot mutate the package-level state.
func (*Agent) ListModels(_ context.Context) ([]string, error) {
	out := make([]string, len(defaultModels))
	copy(out, defaultModels)
	return out, nil
}

// CheckLogin verifies the user is signed in to codex. The CLI's
// `login status` subcommand exits 0 when logged in and non-zero when
// not (the stdout line "Not logged in" is informational only). We
// rely on the exit code rather than parsing stdout so a future
// localisation or wording change does not break the probe.
func (*Agent) CheckLogin(ctx context.Context) error {
	if _, err := run.Output(ctx, Binary, "login", "status"); err != nil {
		return fmt.Errorf(
			"codex login status failed: %w (run 'codex login')",
			err,
		)
	}
	return nil
}

// Plan runs codex against req. Two flavours are supported:
//
//   - Interactive: launch the codex TUI with the prompt as the
//     initial user message (`codex [-m <m>] -- <prompt>`) and the
//     per-task workspace as cmd.Dir. For resume runs we use
//     `codex resume <id> [-m <m>] -- <prompt>` so the picker is
//     skipped and the previous thread continues.
//   - Headless: `codex exec [resume <id>] [-m <m>] --skip-git-repo-check
//     --dangerously-bypass-approvals-and-sandbox -- <prompt>`. The
//     bypass flag auto-approves shell + write tool calls so the
//     non-interactive run never stalls; --skip-git-repo-check lets
//     the per-task workspace (which sits inside .j/tasks/<id>/) host
//     the run even though it is not itself a git repo. The literal
//     `--` pins the prompt as a positional so a leading `-` line in
//     the spec body is not parsed as a flag.
func (a *Agent) Plan(
	ctx context.Context, req codingagents.PlanRequest,
) (int, error) {
	workspace := codingagents.DefaultWorkspace(req.FromFilePath)
	prompt := prompts.PlanPrompt(req)
	return a.runPhase(
		ctx, req.Interactive, workspace, req.ResumeChatID,
		req.Model, prompt, req.AgentLogPath,
	)
}

// Work runs codex against a previously generated plan markdown.
func (a *Agent) Work(
	ctx context.Context, req codingagents.WorkRequest,
) (int, error) {
	workspace := codingagents.DefaultWorkspace(req.PlanPath)
	prompt := prompts.WorkPrompt(req)
	return a.runPhase(
		ctx, req.Interactive, workspace, req.ResumeChatID,
		req.Model, prompt, req.AgentLogPath,
	)
}

// Verify runs codex against the requirements + plan pair. Like the
// other backends the verifier targets the project root workspace so
// it can `git worktree list` the target worktree from there.
func (a *Agent) Verify(
	ctx context.Context, req codingagents.VerifyRequest,
) (int, error) {
	workspace := codingagents.ProjectRootWorkspace()
	prompt := prompts.VerifyPrompt(req)
	return a.runPhase(
		ctx, req.Interactive, workspace, req.ResumeChatID,
		req.Model, prompt, req.AgentLogPath,
	)
}

// runPhase is the shared dispatcher for Plan / Work / Verify. The
// interactive vs headless split is identical across phases — only the
// workspace and prompt differ — so threading them through this helper
// keeps every phase under the 80-line method cap.
func (a *Agent) runPhase(
	ctx context.Context, interactive bool,
	workspace, resumeID, model, prompt, agentLogPath string,
) (int, error) {
	if interactive {
		args := interactiveArgs(resumeID, model, prompt)
		if err := run.RunIn(ctx, workspace, Binary, args...); err != nil {
			return 0, fmt.Errorf("codex: %w", err)
		}
		return 0, nil
	}
	args := headlessArgs(resumeID, model, prompt)
	pid, err := run.SpawnFormattedIn(
		ctx, workspace, agentLogPath, a.FormatLog, Binary, args...,
	)
	if err != nil {
		return 0, fmt.Errorf("codex: %w", err)
	}
	return pid, nil
}

// interactiveArgs returns the argv for the interactive entrypoint.
// Empty resumeID launches a fresh TUI session
// (`codex [-m <m>] -- <prompt>`); a non-empty resumeID continues the
// previous thread (`codex resume <id> [-m <m>] -- <prompt>`).
func interactiveArgs(resumeID, model, prompt string) []string {
	args := leadArgs("", resumeID)
	args = appendModel(args, model)
	return append(args, "--", prompt)
}

// headlessArgs returns the argv tail used by Plan / Work / Verify in
// headless mode: `exec [resume <id>] [-m <m>] --skip-git-repo-check
// --dangerously-bypass-approvals-and-sandbox -- <prompt>`. The bypass
// flag is required for non-interactive runs so codex auto-approves
// shell / write tool calls instead of stalling on a permission
// prompt; --skip-git-repo-check lets the per-task workspace (a plain
// directory under .j/tasks/<id>/) host the run.
func headlessArgs(resumeID, model, prompt string) []string {
	args := leadArgs(argExec, resumeID)
	args = appendModel(args, model)
	return append(args, argSkipGit, argBypass, "--", prompt)
}

// leadArgs returns the leading argv shared by the interactive
// (subcmd="") and headless (subcmd="exec") entrypoints. A non-empty
// resumeID splices `resume <id>` after the optional subcmd so the
// command continues a previously recorded thread.
func leadArgs(subcmd, resumeID string) []string {
	args := make([]string, 0, 4)
	if subcmd != "" {
		args = append(args, subcmd)
	}
	if resumeID != "" {
		args = append(args, argResume, resumeID)
	}
	return args
}

// appendModel threads `-m <model>` onto args when model is non-empty.
// codex tolerates an absent `-m` (falls back to the configured
// default) so empty model yields the slice unchanged rather than
// `-m ""`.
func appendModel(args []string, model string) []string {
	if model == "" {
		return args
	}
	return append(args, argModel, model)
}
