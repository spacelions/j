package testutil

import (
	"context"
	"errors"

	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// ScriptedAgent stands in for a codingagents.Agent in tests. Plan,
// Work, and Verify return errors by default so accidental invocation
// is loud; embedders (e.g. tasks continue tests) override as needed.
type ScriptedAgent struct {
	AgentName string
	Models    []string
	ModelsErr error
	LoginErr  error
}

// NewScriptedAgent returns a cursor-backed fake with two models,
// matching typical CLI test wiring.
func NewScriptedAgent() *ScriptedAgent {
	return &ScriptedAgent{
		AgentName: "cursor",
		Models:    []string{"sonnet-4", "gpt-5"},
	}
}

func (a *ScriptedAgent) Name() string {
	if a.AgentName != "" {
		return a.AgentName
	}
	return "cursor"
}

func (a *ScriptedAgent) ListModels(context.Context) ([]string, error) {
	return a.Models, a.ModelsErr
}

func (a *ScriptedAgent) CheckLogin(context.Context) error { return a.LoginErr }

func (a *ScriptedAgent) NewResumeID(context.Context) (string, error) {
	return "rid", nil
}

func (a *ScriptedAgent) Plan(
	context.Context, codingagents.PlanRequest,
) (int, error) {
	return 0, errors.New("testutil.ScriptedAgent.Plan should not be called")
}

func (a *ScriptedAgent) Work(
	context.Context, codingagents.WorkRequest,
) (int, error) {
	return 0, errors.New("testutil.ScriptedAgent.Work should not be called")
}

func (a *ScriptedAgent) Verify(
	context.Context, codingagents.VerifyRequest,
) (int, error) {
	return 0, errors.New("testutil.ScriptedAgent.Verify should not be called")
}
