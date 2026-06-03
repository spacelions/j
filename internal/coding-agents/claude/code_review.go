package claude

import (
	"context"

	"github.com/spacelions/j/internal/agents/prompts"
	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// CodeReview drives a `j tasks code-review` round. Mirrors Plan's
// flavour split but uses the code-review prompt and the per-task dir
// as the workspace so canonical requirements.md / plan.md are in scope.
func (a *Agent) CodeReview(
	ctx context.Context, req codingagents.CodeReviewRequest,
) (int, error) {
	prompt := prompts.CodeReviewPrompt(req)
	iargs := phaseArgs("", false, argModel, req.Model, prompt)
	hargs := headlessPhaseArgs("", false, req.Model, prompt)
	return a.runPhase(ctx, phaseRun{
		interactive:     req.Interactive,
		workspace:       req.TaskDir,
		agentLogPath:    req.AgentLogPath,
		interactiveArgs: iargs,
		headlessArgs:    hargs,
	})
}
