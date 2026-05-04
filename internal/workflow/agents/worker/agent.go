// Package worker exposes a single New(Config) constructor mirroring
// the planner / verifier shape: Config.LLM → llmagent.New (used by
// `j run` / `j web`), Config{TaskID, Agents} → a custom shell-out
// agent whose Run blocks on cli/work.Run (used by
// `j tasks orchestrate`).
package worker

import (
	"errors"
	"fmt"
	"io"
	"iter"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/spacelions/j/internal/cli/work"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/workflow/instructions"
)

const (
	Name      = "worker"
	OutputKey = "code"
)

// Config carries the runtime knobs New uses to decide which flavour
// of agent to return. Exactly one of LLM and TaskID should be set.
type Config struct {
	LLM    model.LLM
	TaskID string
	Agents []codingagents.Agent
	Stderr io.Writer
}

// New returns the configured worker agent. See planner.New for the
// full Config branching contract.
func New(cfg Config) (agent.Agent, error) {
	if cfg.LLM != nil && cfg.TaskID != "" {
		return nil, errors.New("worker: Config.LLM and Config.TaskID are mutually exclusive")
	}
	if cfg.LLM != nil {
		return llmagent.New(llmagent.Config{
			Name:        Name,
			Model:       cfg.LLM,
			Description: "Produces code from the plan, revising when verifier feedback is available.",
			Instruction: instructions.Worker,
			OutputKey:   OutputKey,
		})
	}
	if cfg.TaskID == "" {
		return nil, errors.New("worker: Config requires LLM or TaskID")
	}
	if len(cfg.Agents) == 0 {
		return nil, errors.New("worker: shell-out branch requires Agents")
	}
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	taskID := cfg.TaskID
	agents := cfg.Agents
	return agent.New(agent.Config{
		Name:        Name,
		Description: "Runs the worker phase by shelling out to `j work` against the seeded task.",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				interactive := false
				if err := work.Run(ctx, work.Options{
					TaskID:            taskID,
					Yes:               true,
					Interactive:       &interactive,
					Stdout:            stderr,
					Stderr:            stderr,
					Agents:            agents,
					WaitForCompletion: true,
				}); err != nil {
					yield(nil, fmt.Errorf("%s: %w", Name, err))
					return
				}
				ev := session.NewEvent(ctx.InvocationID())
				ev.Author = Name
				ev.LLMResponse = model.LLMResponse{
					Content: &genai.Content{
						Role:  genai.RoleUser,
						Parts: []*genai.Part{{Text: "worker phase complete"}},
					},
				}
				yield(ev, nil)
			}
		},
	})
}
