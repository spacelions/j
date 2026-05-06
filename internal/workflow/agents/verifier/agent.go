// Package verifier exposes the verifier agent and its shell-out
// orchestrator (Run, RunResume). New(Config) returns either an
// llmagent (Config.LLM, used by `j run` / `j web`) or a custom
// shell-out agent whose Run calls Run or RunResume based on whether
// the task already has a VerifyResumeSession. The shell-out branch
// flips event.Actions.Escalate=true on `VERDICT: PASS` so a future
// enclosing LoopAgent exits early.
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
	"google.golang.org/genai"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/workflow/instructions"
)

const (
	Name      = "verifier"
	OutputKey = "temp:review"
)

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
// MaxIterations on the shell-out branch (defaulting to
// store.DefaultTaskMaxIterations when zero or negative is supplied).
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
		maxIters = store.DefaultTaskMaxIterations
	}
	taskID := cfg.TaskID
	agents := cfg.Agents
	return agent.New(agent.Config{
		Name:        Name,
		Description: "Runs the verifier phase against the seeded task.",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				t, lookupErr := resolver.TaskByID(taskID)
				var runErr error
				if lookupErr == nil && t.VerifyResumeSession != "" {
					runErr = RunResume(ctx, ResumeOptions{
						TaskID: taskID,
						Stdout: stderr,
						Stderr: stderr,
						Agents: agents,
					})
				} else {
					runErr = Run(ctx, Options{
						TaskID:        taskID,
						Yes:           true,
						Interactive:   false,
						Stdout:        stderr,
						Stderr:        stderr,
						Agents:        agents,
						MaxIterations: maxIters,
					})
				}
				if runErr != nil {
					yield(nil, fmt.Errorf("%s: %w", Name, runErr))
					return
				}
				verdict := resolver.ReadVerdictForTask(taskID)
				ev := session.NewEvent(ctx.InvocationID())
				ev.Author = Name
				ev.LLMResponse = model.LLMResponse{
					Content: &genai.Content{
						Role:  genai.RoleUser,
						Parts: []*genai.Part{{Text: fmt.Sprintf("verifier phase complete (verdict=%s)", verdict)}},
					},
				}
				if verdict == "PASS" {
					ev.Actions.Escalate = true
				}
				yield(ev, nil)
			}
		},
	})
}
