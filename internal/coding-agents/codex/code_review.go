package codex

import (
	"context"
	"fmt"

	"github.com/spacelions/j/internal/agents/prompts"
	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// CodeReview drives a `j tasks code-review` round. It uses a fresh
// session and the per-task dir so the planner can read canonical
// task context while writing only round-local artifacts.
func (a *Agent) CodeReview(
	ctx context.Context, req codingagents.CodeReviewRequest,
) (int, error) {
	prompt := prompts.CodeReviewPrompt(req)
	env, err := prepareScopedEnv(req.TaskDir)
	if err != nil {
		return 0, fmt.Errorf("codex: %w", err)
	}
	return a.runPhase(ctx, phaseRun{
		interactive:     req.Interactive,
		workspace:       req.TaskDir,
		env:             env,
		agentLogPath:    req.AgentLogPath,
		interactiveArgs: interactiveArgs("", req.Model, prompt),
		headlessArgs:    headlessArgs("", req.Model, prompt),
	})
}
