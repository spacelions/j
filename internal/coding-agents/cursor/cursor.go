// Package cursor implements the codingagents.Agent backed by the local
// cursor-agent CLI. Sibling packages under internal/coding-agents/ will
// host other coding-agent backends (Codex, Claude, ...) over time.
package cursor

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// Name implements codingagents.Agent.
func (*Agent) Name() string { return "cursor" }

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

// Plan runs cursor-agent against req. Three flavours are supported:
//
//   - Scratch (req.TargetPath == ""): launch cursor-agent in plan mode
//     with no prompt and no workspace. The user drives the session
//     freely; nothing is written to disk by us.
//   - Markdown interactive (req.Interactive, non-empty TargetPath):
//     launch cursor's TUI (no --print, no --mode) and ask cursor to
//     save req.OutputPath before exiting via a suffix on the prompt.
//   - Markdown headless: --print --output-format text --mode plan,
//     capture stdout, write the file from Go.
func (*Agent) Plan(ctx context.Context, req codingagents.PlanRequest) error {
	if req.TargetPath == "" {
		if err := run.Run(ctx, Binary,
			"--mode", "plan",
			"--model", req.Model,
		); err != nil {
			return fmt.Errorf("cursor-agent: %w", err)
		}
		return nil
	}

	workspace := codingagents.DefaultWorkspace(req.TargetPath)
	base := prompts.BuildPlanner(req.TargetPath, req.Body)

	if req.Interactive {
		prompt := fmt.Sprintf(
			"%s\n\nWhen the plan is final, save it to %q (overwrite if it exists), then exit.",
			base, req.OutputPath,
		)
		if err := run.Run(ctx, Binary,
			"--model", req.Model,
			"--workspace", workspace,
			prompt,
		); err != nil {
			return fmt.Errorf("cursor-agent: %w", err)
		}
		return nil
	}

	out, err := run.Output(ctx, Binary,
		"--print",
		"--output-format", "text",
		"--mode", "plan",
		"--model", req.Model,
		"--workspace", workspace,
		base,
	)
	if err != nil {
		return fmt.Errorf("cursor-agent: %w", err)
	}
	plan := strings.TrimSpace(out)
	if plan == "" {
		return errors.New("cursor-agent returned an empty plan")
	}
	if err := os.WriteFile(req.OutputPath, []byte(plan+"\n"), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", req.OutputPath, err)
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
		if err := run.Run(ctx, Binary,
			"--model", req.Model,
			"--workspace", workspace,
			prompt,
		); err != nil {
			return fmt.Errorf("cursor-agent: %w", err)
		}
		return nil
	}

	if _, err := run.Output(ctx, Binary,
		"--print",
		"--output-format", "text",
		"--model", req.Model,
		"--workspace", workspace,
		prompt,
	); err != nil {
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
