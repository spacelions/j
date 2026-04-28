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
	"github.com/spacelions/j/internal/util/strs"
)

// Binary is the cursor-agent executable name.
const Binary = "cursor-agent"

// Agent is a Cursor-backed planner.
type Agent struct {
	runner run.Runner
}

// New returns a Cursor agent that shells out to the cursor-agent CLI.
func New() *Agent {
	return &Agent{runner: run.NewExec()}
}

// NewWithRunner lets tests inject a scripted runner.
func NewWithRunner(r run.Runner) *Agent {
	return &Agent{runner: r}
}

// Name implements codingagents.Agent.
func (*Agent) Name() string { return "cursor" }

// ListModels asks cursor-agent for the available model identifiers.
func (a *Agent) ListModels(ctx context.Context) ([]string, error) {
	out, err := a.runner.Output(ctx, Binary, "--list-models")
	if err != nil {
		return nil, fmt.Errorf("list cursor models: %w (run 'cursor-agent login' or check your account)", err)
	}
	models, err := strs.ParseList(out, "no models")
	if err != nil {
		// strs.ParseList only fails with ErrEmptyList today; surface the
		// cursor-flavored remediation hint.
		return nil, errors.New("cursor-agent returned no models")
	}
	return models, nil
}

// CheckLogin verifies the user is signed in to cursor-agent. The CLI
// prints a "Logged in" line on success; phrases like "Not logged in" or
// "logged out" are treated as a logged-out state and surface a remediation
// hint pointing at `cursor-agent login`.
func (a *Agent) CheckLogin(ctx context.Context) error {
	out, err := a.runner.Output(ctx, Binary, "status")
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

// Plan invokes cursor-agent in plan mode against the given target and
// model and returns the captured plan text.
func (a *Agent) Plan(ctx context.Context, req codingagents.PlanRequest) (string, error) {
	prompt := codingagents.BuildPrompt(req.TargetPath, req.Body)
	workspace := codingagents.DefaultWorkspace(req.TargetPath)
	out, err := a.runner.Output(ctx, Binary,
		"--print",
		"--output-format", "text",
		"--mode", "plan",
		"--model", req.Model,
		"--workspace", workspace,
		prompt,
	)
	if err != nil {
		return "", fmt.Errorf("cursor-agent: %w", err)
	}
	plan := strings.TrimSpace(out)
	if plan == "" {
		return "", errors.New("cursor-agent returned an empty plan")
	}
	return plan, nil
}
