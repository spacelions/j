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
	"github.com/spacelions/j/internal/workflow/prompts"
)

// Binary is the cursor-agent executable name.
const Binary = "cursor-agent"

// Agent is a Cursor-backed planner. It is stateless: every method shells
// out to the real cursor-agent binary on PATH via the run package's
// package-level helpers. Tests drive it with a stub binary on PATH
// rather than an injected runner (see AGENTS.md "no test seams" rule).
type Agent struct{}

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
		return nil, fmt.Errorf("list cursor models: %w (run 'cursor-agent login' or check your account)", err)
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
		return fmt.Errorf("cursor-agent status failed: %w (run 'cursor-agent login')", err)
	}
	lower := strings.ToLower(out)
	switch {
	case strings.Contains(lower, "not logged"),
		strings.Contains(lower, "logged out"),
		strings.Contains(lower, "not authenticated"),
		strings.Contains(lower, "signed out"):
		return errors.New("cursor-agent reports not logged in; run 'cursor-agent login'")
	}
	if !strings.Contains(lower, "logged in") {
		return errors.New("cursor-agent reports not logged in; run 'cursor-agent login'")
	}
	return nil
}

// Plan runs cursor-agent against req. The agent saves both the
// (possibly refined) requirements summary and the final plan into the
// per-task folder before exiting; the orchestrator reads them after.
//
// Two flavours are supported:
//
//   - Interactive (req.Interactive == true): launch cursor's TUI
//     (no --print, no --mode) and ask cursor to save both files before
//     exiting via a suffix on the prompt.
//   - Headless (req.Interactive == false): --print --output-format text
//     --mode plan, with the same save-instruction suffix on the
//     prompt; cursor writes the files via its tool use and the
//     captured stdout is discarded.
func (*Agent) Plan(ctx context.Context, req codingagents.PlanRequest) error {
	workspace := codingagents.DefaultWorkspace(req.FromFilePath)
	base := prompts.BuildPlanner(req.FromFilePath, req.Body)
	prompt := fmt.Sprintf(
		"%s\n\nDuring this session you may clarify the requirements with the user. Before exiting:\n"+
			"1. Save the (possibly refined) requirements summary to %q (overwrite if it exists).\n"+
			"2. Save the plan to %q (overwrite if it exists).\n"+
			"Then exit.",
		base, req.RequirementsOutputPath, req.PlanOutputPath,
	)

	if req.Interactive {
		var args []string
		if req.ResumeChatID != "" {
			args = append(args, "--resume", req.ResumeChatID)
		}
		args = append(args, "--model", req.Model, "--workspace", workspace, prompt)
		if err := run.Run(ctx, Binary, args...); err != nil {
			return fmt.Errorf("cursor-agent: %w", err)
		}
		return nil
	}

	var hargs []string
	if req.ResumeChatID != "" {
		hargs = append(hargs, "--resume", req.ResumeChatID)
	}
	hargs = append(hargs, "--print", "--output-format", "text", "--mode", "plan", "--model", req.Model, "--workspace", workspace, prompt)
	if _, err := run.Output(ctx, Binary, hargs...); err != nil {
		return fmt.Errorf("cursor-agent: %w", err)
	}
	return nil
}

// Work runs cursor-agent against a previously generated plan markdown.
// The agent edits files in the plan's directory directly, so we do not
// pass --mode plan and we do not capture stdout for a file write. Two
// flavours are supported:
//
//   - Interactive: launch cursor's TUI with the coder prompt as the
//     initial user message; the user drives the session until cursor
//     exits.
//   - Headless: --print --output-format text against the coder prompt,
//     letting cursor edit files and print a brief summary that we
//     discard.
func (*Agent) Work(ctx context.Context, req codingagents.WorkRequest) error {
	workspace := codingagents.DefaultWorkspace(req.PlanPath)
	prompt := prompts.BuildCoder(req.PlanPath, req.Body)

	if req.Interactive {
		var wargs []string
		if req.ResumeChatID != "" {
			wargs = append(wargs, "--resume", req.ResumeChatID)
		}
		wargs = append(wargs, "--model", req.Model, "--workspace", workspace, prompt)
		if err := run.Run(ctx, Binary, wargs...); err != nil {
			return fmt.Errorf("cursor-agent: %w", err)
		}
		return nil
	}

	pargs := []string{"--print", "--output-format", "text", "--model", req.Model, "--workspace", workspace, prompt}
	if req.ResumeChatID != "" {
		pargs = append([]string{"--resume", req.ResumeChatID}, pargs...)
	}
	if _, err := run.Output(ctx, Binary, pargs...); err != nil {
		return fmt.Errorf("cursor-agent: %w", err)
	}
	return nil
}

// parseModels extracts cursor-agent model IDs from --list-models output.
// The CLI prints a header banner ("Available models") followed by lines
// of the form "<id> - <Display Name>" (sometimes with a "(default)"
// annotation). Lines without a " - " separator are treated as banner
// noise and skipped; for matching lines we keep only the id.
func parseModels(out string) []string {
	var ids []string
	for _, raw := range strings.Split(out, "\n") {
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
