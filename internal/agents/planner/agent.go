// Package planner exposes a single New(Config) constructor that
// returns either an LLMAgent (Config.LLM set) or a shell-out custom
// agent (Config.TaskID + Agents set) whose Run blocks on Execute.
// The same shape covers `j run` / `j web` (LLM) and `j tasks
// orchestrate` (shell-out) so a future LLM-backed orchestrator is
// a Config field away.
package planner

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

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/agents/instructions"
)

const (
	Name      = "planner"
	OutputKey = "plan"
)

// Config carries the runtime knobs New uses to decide which flavour
// of agent to return. Exactly one of LLM and TaskID should be set; the
// constructor errors if both / neither are populated.
type Config struct {
	// LLM, when non-nil, switches New to the LLMAgent flavour
	// used by `j run` / `j web`. The model is forwarded verbatim to
	// llmagent.Config.
	LLM model.LLM

	// TaskID, when non-empty, switches New to the shell-out flavour
	// used by `j tasks orchestrate`. Agents must be supplied;
	// Stderr defaults to io.Discard when nil.
	TaskID string
	Agents []codingagents.Agent
	Stderr io.Writer

	// Tool and Model are one-off overrides forwarded from
	// `j tasks orchestrate --tool/--model`. When non-empty, resolver.Agent
	// uses them instead of (or to supplement) the stored bucket values.
	Tool  string
	Model string

	// Interactive controls whether the planner runs in interactive (TUI)
	// mode. Defaults to false for the headless orchestrator path.
	Interactive bool

	// Yes, when true, is forwarded into resolver.Agent so any
	// status-mismatch confirmation is skipped automatically.
	Yes bool
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
	tool := cfg.Tool
	agentModel := cfg.Model
	interactive := cfg.Interactive
	return agent.New(agent.Config{
		Name:        Name,
		Description: "Runs the planner phase via Execute against the seeded task.",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				resolvedAgent, resolvedModel, err := resolver.Agent(ctx, resolver.AgentOptions{
					Bucket:        store.BucketPlanner,
					Agents:        agents,
					ExplicitTool:  tool,
					ExplicitModel: agentModel,
					Stderr:        stderr,
					Interactive:   interactive,
				})
				if err != nil {
					yield(nil, fmt.Errorf("%s: %w", Name, err))
					return
				}
				if err := Execute(ctx, ExecuteOptions{
					TaskID:            taskID,
					Agent:             resolvedAgent,
					Model:             resolvedModel,
					Interactive:       interactive,
					WaitForCompletion: true,
					Stderr:            stderr,
				}); err != nil {
					yield(nil, fmt.Errorf("%s: %w", Name, err))
					return
				}
				ev := session.NewEvent(ctx.InvocationID())
				ev.Author = Name
				ev.LLMResponse = model.LLMResponse{
					Content: &genai.Content{
						Role:  genai.RoleUser,
						Parts: []*genai.Part{{Text: "planner phase complete"}},
					},
				}
				yield(ev, nil)
			}
		},
	})
}
