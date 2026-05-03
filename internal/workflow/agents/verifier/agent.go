// Package verifier exposes a single New(Config) constructor mirroring
// the planner / worker shape: Config.LLM → llmagent.New (used by
// `j run` / `j web`), Config{TaskID, Agents, MaxIterations} → a
// custom shell-out agent whose Run blocks on cli/verify.Run (used by
// `j tasks orchestrate`). The shell-out branch flips
// event.Actions.Escalate=true on `VERDICT: PASS` so a future
// enclosing LoopAgent exits early instead of running to its own
// MaxIterations.
package verifier

import (
	"errors"
	"fmt"
	"io"
	"iter"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"

	"github.com/spacelions/j/internal/cli/verify"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/workflow/agents/shellevent"
	"github.com/spacelions/j/internal/workflow/instructions"
)

const (
	Name      = "verifier"
	OutputKey = "temp:review"
)

// defaultMaxIterations matches `j verify`'s internal default and is
// the fallback when the workflow caller passes 0 / negative.
const defaultMaxIterations = 3

// Config carries the runtime knobs New uses to decide which flavour
// of agent to return. Exactly one of LLM and TaskID should be set.
type Config struct {
	LLM           model.LLM
	TaskID        string
	Agents        []codingagents.Agent
	Stderr        io.Writer
	MaxIterations int
}

// New returns the configured verifier agent. See planner.New for the
// full Config branching contract; the verifier additionally consumes
// MaxIterations on the shell-out branch (defaulting to 3 when zero
// or negative is supplied).
func New(cfg Config) (agent.Agent, error) {
	if cfg.LLM != nil && cfg.TaskID != "" {
		return nil, errors.New("verifier: Config.LLM and Config.TaskID are mutually exclusive")
	}
	if cfg.LLM != nil {
		return llmagent.New(llmagent.Config{
			Name:        Name,
			Model:       cfg.LLM,
			Description: "Reviews the worker's output against the plan and returns a concise verdict.",
			Instruction: instructions.Verifier,
			OutputKey:   OutputKey,
		})
	}
	if cfg.TaskID == "" {
		return nil, errors.New("verifier: Config requires LLM or TaskID")
	}
	if len(cfg.Agents) == 0 {
		return nil, errors.New("verifier: shell-out branch requires Agents")
	}
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	maxIters := cfg.MaxIterations
	if maxIters <= 0 {
		maxIters = defaultMaxIterations
	}
	taskID := cfg.TaskID
	agents := cfg.Agents
	return agent.New(agent.Config{
		Name:        Name,
		Description: "Runs the verifier phase by shelling out to `j verify` against the seeded task.",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				interactive := false
				err := verify.Run(ctx, verify.Options{
					TaskID:        taskID,
					Yes:           true,
					Interactive:   &interactive,
					Stdout:        stderr,
					Stderr:        stderr,
					Agents:        agents,
					MaxIterations: maxIters,
				})
				if err != nil {
					yield(nil, fmt.Errorf("%s: %w", Name, err))
					return
				}
				verdict := verify.ReadVerdictForTask(taskID)
				shellevent.Yield(ctx, yield, Name,
					fmt.Sprintf("verifier phase complete (verdict=%s)", verdict),
					nil, verdict == "PASS")
			}
		},
	})
}
