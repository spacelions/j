// Package cursor implements the codingagents.Agent backed by the local
// cursor-agent CLI. Sibling packages under internal/coding-agents/ will
// host other coding-agent backends (Codex, Claude, ...) over time.
package cursor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/agents/prompts"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/util/run"
)

// Binary is the cursor-agent executable name.
const Binary = "cursor-agent"

const (
	argPrint                  = "--print"
	argOutputFormat           = "--output-format"
	argOutputFormatStreamJSON = "stream-json"
	argForce                  = "--force"
	argTrust                  = "--trust"
	argWorkspace              = "--workspace"
	argResume                 = "--resume"
	argModel                  = "--model"
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
// requirements.md / plan.md into the per-task folder before exiting.
// Interactive launches the TUI with --mode plan; headless drops it
// (read-only blocks the save instructions) and adds --force --trust
// to auto-approve tool calls. Headless also asks for `--output-format
// stream-json` so cursor-agent emits one JSON event per assistant
// content block / tool call / tool result; the run helper renders
// each event as an agentlog-style marker line via Agent.FormatLog
// before it lands in req.AgentLogPath. `--stream-partial-output` was
// dropped in SPA-73: each text delta would otherwise produce its own
// envelope (30–200+ per turn) and the formatter has nothing useful to
// add over the aggregated per-block events.
func (a *Agent) Plan(
	ctx context.Context, req codingagents.PlanRequest,
) (int, error) {
	workspace := codingagents.DefaultWorkspace(req.FromFilePath)
	prompt := prompts.PlanPrompt(req)

	iargs := phaseArgs(req.ResumeChatID,
		"--mode", "plan", argModel, req.Model,
		argWorkspace, workspace, prompt,
	)
	hargs := phaseArgs(req.ResumeChatID,
		argPrint,
		argOutputFormat, argOutputFormatStreamJSON,
		argForce, argTrust, argModel, req.Model,
		argWorkspace, workspace, prompt,
	)
	return a.runPhase(ctx, req.Interactive, req.AgentLogPath, iargs, hargs)
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
//   - Headless: same flag set as Plan's headless branch (stream-json,
//     force, trust), fire-and-forget. --force / --trust auto-approve
//     tool calls and the workspace trust prompt; stdout / stderr go
//     through run.SpawnFormattedIn so each stream-json event lands in
//     req.AgentLogPath as an agentlog-style marker line. The spawned
//     PID is returned. The interactive path returns 0.
func (a *Agent) Work(
	ctx context.Context, req codingagents.WorkRequest,
) (int, error) {
	workspace := codingagents.DefaultWorkspace(req.PlanPath)
	prompt := prompts.WorkPrompt(req)

	iargs := phaseArgs(req.ResumeChatID,
		argModel, req.Model,
		argWorkspace, workspace, prompt,
	)
	hargs := phaseArgs(req.ResumeChatID,
		argPrint,
		argOutputFormat, argOutputFormatStreamJSON,
		argForce, argTrust, argModel, req.Model,
		argWorkspace, workspace, prompt,
	)
	return a.runPhase(ctx, req.Interactive, req.AgentLogPath, iargs, hargs)
}

// Verify runs cursor-agent against the requirements + plan pair.
// The agent saves verifier_plan.md / verifier_findings.md before
// exiting; the orchestrator reads them after. Interactive launches
// the TUI without --mode plan (the verifier needs writes); headless
// uses Work's flag set (stream-json via run.SpawnFormattedIn).
// Verify runs with `--workspace <project-root>` so the verifier can
// `git worktree list` the target worktree — Plan and Work use
// DefaultWorkspace for their self-contained per-task dir.
func (a *Agent) Verify(
	ctx context.Context, req codingagents.VerifyRequest,
) (int, error) {
	workspace := codingagents.ProjectRootWorkspace()
	prompt := prompts.VerifyPrompt(req)

	iargs := phaseArgs(req.ResumeChatID,
		argModel, req.Model,
		argWorkspace, workspace, prompt,
	)
	hargs := phaseArgs(req.ResumeChatID,
		argPrint,
		argOutputFormat, argOutputFormatStreamJSON,
		argForce, argTrust, argModel, req.Model,
		argWorkspace, workspace, prompt,
	)
	return a.runPhase(ctx, req.Interactive, req.AgentLogPath, iargs, hargs)
}

func (a *Agent) runPhase(
	ctx context.Context, interactive bool, agentLogPath string,
	interactiveArgs, headlessArgs []string,
) (int, error) {
	if interactive {
		if err := run.Run(ctx, Binary, interactiveArgs...); err != nil {
			return 0, fmt.Errorf("cursor-agent: %w", err)
		}
		return 0, nil
	}
	pid, err := run.SpawnFormattedIn(
		ctx, "", agentLogPath,
		a.FormatLog, Binary, headlessArgs...,
	)
	if err != nil {
		return 0, fmt.Errorf("cursor-agent: %w", err)
	}
	return pid, nil
}

func phaseArgs(resumeID string, args ...string) []string {
	if resumeID == "" {
		return args
	}
	return append([]string{argResume, resumeID}, args...)
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
