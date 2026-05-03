// Package planner exposes a single New(Config) constructor that
// returns either an LLMAgent (Config.LLM set) or a shell-out custom
// agent (Config.TaskID + Agents set) whose Run blocks on cli/plan.Run.
// The same shape covers `j run` / `j web` (LLM) and `j tasks
// orchestrate` (shell-out) so a future LLM-backed orchestrator is
// a Config field away.
package planner

import (
	"errors"
	"io"
	"iter"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"

	"github.com/spacelions/j/internal/cli/plan"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/workflow/agents/shellevent"
	"github.com/spacelions/j/internal/workflow/instructions"
)

const (
	Name      = "planner"
	OutputKey = "plan"
)

// Config carries the runtime knobs New uses to decide which flavour
// of agent to return. Exactly one of LLM and TaskID should be set; the
// constructor errors if both / neither are populated.
type Config struct {
	// LLM, when non-nil, switches New to the today-LLMAgent flavour
	// used by `j run` / `j web`. The model is forwarded verbatim to
	// llmagent.Config.
	LLM model.LLM

	// TaskID, when non-empty, switches New to the shell-out flavour
	// used by `j tasks orchestrate`. Agents must be supplied;
	// Stderr defaults to io.Discard when nil.
	TaskID string
	Agents []codingagents.Agent
	Stderr io.Writer
}

// New returns the configured planner agent. The empty / ambiguous
// cases (both LLM and TaskID set, or neither set, or shell-out branch
// missing agents) surface as errors so misuse is loud at construction
// rather than at first Run.
func New(cfg Config) (agent.Agent, error) {
	if cfg.LLM != nil && cfg.TaskID != "" {
		return nil, errors.New("planner: Config.LLM and Config.TaskID are mutually exclusive")
	}
	if cfg.LLM != nil {
		return llmagent.New(llmagent.Config{
			Name:        Name,
			Model:       cfg.LLM,
			Description: "Breaks the user's request into a concrete, ordered implementation plan.",
			Instruction: instructions.Planner,
			OutputKey:   OutputKey,
		})
	}
	if cfg.TaskID == "" {
		return nil, errors.New("planner: Config requires LLM or TaskID")
	}
	if len(cfg.Agents) == 0 {
		return nil, errors.New("planner: shell-out branch requires Agents")
	}
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	taskID := cfg.TaskID
	agents := cfg.Agents
	return agent.New(agent.Config{
		Name:        Name,
		Description: "Runs the planner phase by shelling out to `j plan` against the seeded task.",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				interactive := false
				err := plan.Run(ctx, plan.Options{
					TaskID:            taskID,
					Yes:               true,
					Interactive:       &interactive,
					Stdout:            stderr,
					Stderr:            stderr,
					Agents:            agents,
					WaitForCompletion: true,
				})
				shellevent.Yield(ctx, yield, Name, "planner phase complete", err, false)
			}
		},
	})
}
