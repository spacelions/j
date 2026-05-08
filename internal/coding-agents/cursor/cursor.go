// Package cursor implements the codingagents.Agent backed by the local
// cursor-agent CLI. Sibling packages under internal/coding-agents/ will
// host other coding-agent backends (Codex, Claude, ...) over time.
package cursor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/util/run"
)

// Binary is the cursor-agent executable name.
const Binary = "cursor-agent"

const (
	argPrint            = "--print"
	argOutputFormat     = "--output-format"
	argOutputFormatText = "text"
	argForce            = "--force"
	argTrust            = "--trust"
	argWorkspace        = "--workspace"
	argResume           = "--resume"
	argModel            = "--model"
)

// Agent is a Cursor-backed planner. It is stateless: every method shells
// out to the real cursor-agent binary on PATH via the run package's
// package-level helpers. Tests drive it with a stub binary on PATH
// rather than an injected runner (see AGENTS.md "no test seams" rule).
type Agent struct{}

var _ codingagents.Agent = (*Agent)(nil)

// New returns a Cursor agent that shells out to the cursor-agent CLI.
func New() *Agent { return &Agent{} }

// CreateChatID runs `cursor-agent create-chat` and returns the new chat
// session id (a UUID) printed on stdout. The caller should pass that id
// in PlanRequest.ResumeChatID or WorkRequest.ResumeChatID and retain the
// same value in the task log so `j tasks` can show how to resume with
// `cursor-agent --resume <id>`.
func CreateChatID(ctx context.Context) (string, error) {
	out, err := run.Output(ctx, Binary, "create-chat")
	if err != nil {
		return "", fmt.Errorf("create-chat: %w", err)
	}
	id := strings.TrimSpace(out)
	if id == "" {
		return "", errors.New("create-chat: empty id")
	}
	return id, nil
}

// Name implements codingagents.Agent.
func (*Agent) Name() string { return "cursor" }

// NewResumeID returns a fresh `cursor-agent create-chat` id. It is
// the codingagents.Agent-level entry point; cmd packages call it
// instead of sniffing agent.Name() == "cursor". CreateChatID stays
// exported because the existing posix tests exercise it directly.
func (*Agent) NewResumeID(ctx context.Context) (string, error) {
	return CreateChatID(ctx)
}

// ListModels asks cursor-agent for the available model identifiers.
func (*Agent) ListModels(ctx context.Context) ([]string, error) {
	out, err := run.Output(ctx, Binary, "--list-models")
	if err != nil {
		return nil, fmt.Errorf(
			"list cursor models: %w "+
				"(run 'cursor-agent login' or check your account)",
			err,
		)
	}
	models := parseModels(out)
	if len(models) == 0 {
		return nil, errors.New("cursor-agent returned no models")
	}
	return models, nil
}

// CheckLogin verifies the user is signed in to cursor-agent. The CLI
// prints a "Logged in" line on success; phrases like "Not logged in" or
// "logged out" are treated as a logged-out state and surface a remediation
// hint pointing at `cursor-agent login`.
func (*Agent) CheckLogin(ctx context.Context) error {
	out, err := run.Output(ctx, Binary, "status")
	if err != nil {
		return fmt.Errorf(
			"cursor-agent status failed: %w "+
				"(run 'cursor-agent login')",
			err,
		)
	}
	lower := strings.ToLower(out)
	switch {
	case strings.Contains(lower, "not logged"),
		strings.Contains(lower, "logged out"),
		strings.Contains(lower, "not authenticated"),
		strings.Contains(lower, "signed out"):
		return errors.New(
			"cursor-agent reports not logged in; " +
				"run 'cursor-agent login'",
		)
	}
	if !strings.Contains(lower, "logged in") {
		return errors.New(
			"cursor-agent reports not logged in; " +
				"run 'cursor-agent login'",
		)
	}
	return nil
}

// Plan runs cursor-agent against req. The agent saves
// requirements.md / plan.md into the per-task folder before exiting;
// the orchestrator reads them after. Interactive launches the TUI
// with --mode plan; headless drops --mode plan (it is read-only and
// blocks the save instructions) and adds --force --trust to
// auto-approve tool calls. Headless is fire-and-forget: stdio is
// redirected to req.AgentLogPath and the spawned PID is returned.
func (*Agent) Plan(
	ctx context.Context, req codingagents.PlanRequest,
) (int, error) {
	workspace := codingagents.DefaultWorkspace(req.FromFilePath)
	prompt := buildPlanPrompt(req)

	if req.Interactive {
		var args []string
		if req.ResumeChatID != "" {
			args = append(args, argResume, req.ResumeChatID)
		}
		args = append(args,
			"--mode", "plan", argModel, req.Model,
			argWorkspace, workspace, prompt,
		)
		if err := run.Run(ctx, Binary, args...); err != nil {
			return 0, fmt.Errorf("cursor-agent: %w", err)
		}
		return 0, nil
	}

	var hargs []string
	if req.ResumeChatID != "" {
		hargs = append(hargs, argResume, req.ResumeChatID)
	}
	hargs = append(hargs,
		argPrint, argOutputFormat, argOutputFormatText,
		argForce, argTrust, argModel, req.Model,
		argWorkspace, workspace, prompt,
	)
	pid, err := run.Spawn(ctx, req.AgentLogPath, Binary, hargs...)
	if err != nil {
		return 0, fmt.Errorf("cursor-agent: %w", err)
	}
	return pid, nil
}

// Work runs cursor-agent against a previously generated plan markdown.
// The agent edits files in the plan's directory directly, so we do not
// pass --mode plan and we do not capture stdout for a file write. Two
// flavours are supported:
//
//   - Interactive: launch cursor's TUI with the worker prompt as the
//     initial user message; the user drives the session until cursor
//     exits. The interactive branch does not gain --force/--trust
//     because the user can approve tool calls and the workspace
//     trust prompt manually in the TUI.
//   - Headless: --print --output-format text --force --trust against
//     the worker prompt, fire-and-forget. --force auto-approves tool
//     calls (no interactive approval prompts) and --trust skips the
//     workspace trust prompt (it only takes effect with
//     --print/headless); together they make `j work` against cursor
//     actually run end-to-end without stalling on prompts.
//     cursor-agent's stdout/stderr are redirected to req.AgentLogPath
//     via run.Spawn and the spawned PID is returned so `j work` can
//     record it for later reaping. The interactive path stays
//     synchronous and returns 0.
func (*Agent) Work(
	ctx context.Context, req codingagents.WorkRequest,
) (int, error) {
	workspace := codingagents.DefaultWorkspace(req.PlanPath)
	prompt := buildWorkPrompt(req)

	if req.Interactive {
		var wargs []string
		if req.ResumeChatID != "" {
			wargs = append(wargs, argResume, req.ResumeChatID)
		}
		wargs = append(wargs,
			argModel, req.Model,
			argWorkspace, workspace, prompt,
		)
		if err := run.Run(ctx, Binary, wargs...); err != nil {
			return 0, fmt.Errorf("cursor-agent: %w", err)
		}
		return 0, nil
	}

	pargs := []string{
		argPrint, argOutputFormat, argOutputFormatText,
		argForce, argTrust, argModel, req.Model,
		argWorkspace, workspace, prompt,
	}
	if req.ResumeChatID != "" {
		pargs = append([]string{argResume, req.ResumeChatID}, pargs...)
	}
	pid, err := run.Spawn(ctx, req.AgentLogPath, Binary, pargs...)
	if err != nil {
		return 0, fmt.Errorf("cursor-agent: %w", err)
	}
	return pid, nil
}

// Verify runs cursor-agent against the requirements + plan pair. The
// agent saves the draft verifier plan and the findings markdown
// before exiting; the orchestrator reads the findings afterwards to
// derive the VERDICT verdict. Two flavours mirror Plan / Work:
//
//   - Interactive: launch cursor's TUI without --mode plan. The
//     verifier must edit verifier_plan.md / verifier_findings.md
//     and (on FAIL) project files, so plan mode would block those
//     writes. The interactive branch does not gain --force/--trust;
//     the user can approve writes manually in the TUI.
//   - Headless: --print --output-format text --force --trust against
//     the verifier prompt, fire-and-forget. Same headless flag set
//     as Work; --mode plan is intentionally absent for the same
//     reason: the verifier needs write access to its output files.
//
// Unlike Plan and Work, Verify runs with `--workspace <project-root>`
// (not `.j/tasks/<id>/`): the verifier inspects the worktree named
// in req.Worktree via `git worktree list`, which only works from the
// repository's main checkout. Plan and Work still use
// DefaultWorkspace because they want the self-contained per-task
// folder.
func (*Agent) Verify(
	ctx context.Context, req codingagents.VerifyRequest,
) (int, error) {
	workspace := codingagents.ProjectRootWorkspace()
	prompt := buildVerifyPrompt(req)

	if req.Interactive {
		var args []string
		if req.ResumeChatID != "" {
			args = append(args, argResume, req.ResumeChatID)
		}
		args = append(args,
			argModel, req.Model,
			argWorkspace, workspace, prompt,
		)
		if err := run.Run(ctx, Binary, args...); err != nil {
			return 0, fmt.Errorf("cursor-agent: %w", err)
		}
		return 0, nil
	}

	pargs := []string{
		argPrint, argOutputFormat, argOutputFormatText,
		argForce, argTrust, argModel, req.Model,
		argWorkspace, workspace, prompt,
	}
	if req.ResumeChatID != "" {
		pargs = append([]string{argResume, req.ResumeChatID}, pargs...)
	}
	pid, err := run.Spawn(ctx, req.AgentLogPath, Binary, pargs...)
	if err != nil {
		return 0, fmt.Errorf("cursor-agent: %w", err)
	}
	return pid, nil
}

// parseModels extracts cursor-agent model IDs from --list-models output.
// The CLI prints a header banner ("Available models") followed by lines
// of the form "<id> - <Display Name>" (sometimes with a "(default)"
// annotation). Lines without a " - " separator are treated as banner
// noise and skipped; for matching lines we keep only the id.
func parseModels(out string) []string {
	var ids []string
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		i := strings.Index(line, " - ")
		if i <= 0 {
			continue
		}
		if id := strings.TrimSpace(line[:i]); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
