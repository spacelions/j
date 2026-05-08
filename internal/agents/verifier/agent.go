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

	"github.com/spacelions/j/internal/agents/prompts"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
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
		return nil, errors.New(
			"verifier: Config.LLM and Config.TaskID are mutually exclusive",
		)
	}
	if cfg.LLM != nil {
		return newLLMVerifier(cfg)
	}
	if cfg.TaskID == "" {
		return nil, errors.New("verifier: Config requires LLM or TaskID")
	}
	if len(cfg.Agents) == 0 {
		return nil, errors.New(
			"verifier: shell-out branch requires Agents",
		)
	}
	return newShellOutVerifier(cfg), nil
}

// newLLMVerifier builds the LLMAgent flavour returned by New when
// Config.LLM is set. Split out so New stays under the 80-line method
// cap enforced by the pre-commit hook.
func newLLMVerifier(cfg Config) (agent.Agent, error) {
	return llmagent.New(llmagent.Config{
		Name:  Name,
		Model: cfg.LLM,
		Description: "Reviews the worker's output against the plan " +
			"and returns a concise verdict.",
		Instruction: prompts.Resolve(store.BucketVerifier),
		OutputKey:   OutputKey,
	})
}

// newShellOutVerifier builds the custom shell-out agent returned by
// New when Config.TaskID is set. The Run closure captures the
// resolved knobs so each invocation drives Run / RunResume against a
// stable task identity.
func newShellOutVerifier(cfg Config) agent.Agent {
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
	a, _ := agent.New(agent.Config{
		Name:        Name,
		Description: "Runs the verifier phase against the seeded task.",
		Run:         shellOutRun(taskID, agents, stderr, maxIters),
	})
	return a
}

// shellOutRun returns the iter.Seq2 closure used by the shell-out
// verifier branch. Extracting it keeps newShellOutVerifier slim and
// makes the dispatch (RunResume vs Run) easy to follow.
func shellOutRun(
	taskID string,
	agents []codingagents.Agent,
	stderr io.Writer,
	maxIters int,
) func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			runErr := dispatchShellOut(
				ctx, taskID, agents, stderr, maxIters,
			)
			if runErr != nil {
				yield(nil, fmt.Errorf("%s: %w", Name, runErr))
				return
			}
			yield(verdictEvent(ctx, taskID), nil)
		}
	}
}

// dispatchShellOut chooses between Run and RunResume for the shell-out
// branch based on whether the task has a recorded resume session.
func dispatchShellOut(
	ctx agent.InvocationContext,
	taskID string,
	agents []codingagents.Agent,
	stderr io.Writer,
	maxIters int,
) error {
	t, lookupErr := resolver.TaskByID(taskID)
	if lookupErr == nil && t.VerifyResumeSession != "" {
		return RunResume(ctx, ResumeOptions{
			TaskID: taskID,
			Stdout: stderr,
			Stderr: stderr,
			Agents: agents,
		})
	}
	return Run(ctx, Options{
		TaskID:        taskID,
		Yes:           true,
		Interactive:   false,
		Stdout:        stderr,
		Stderr:        stderr,
		Agents:        agents,
		MaxIterations: maxIters,
	})
}

// verdictEvent constructs the trailing session.Event yielded by the
// shell-out verifier branch, including the Escalate flag set on
// VERDICT: PASS so an enclosing LoopAgent exits early.
func verdictEvent(
	ctx agent.InvocationContext, taskID string,
) *session.Event {
	verdict := resolver.ReadVerdictForTask(taskID)
	ev := session.NewEvent(ctx.InvocationID())
	ev.Author = Name
	ev.LLMResponse = model.LLMResponse{
		Content: &genai.Content{
			Role: genai.RoleUser,
			Parts: []*genai.Part{{
				Text: fmt.Sprintf(
					"verifier phase complete (verdict=%s)", verdict,
				),
			}},
		},
	}
	if verdict == resolver.VerdictPass {
		ev.Actions.Escalate = true
	}
	return ev
}
