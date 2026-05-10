package verifier

import (
	"context"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/util/run"
)

// buildVerifyReq composes the per-iteration VerifyRequest.
func buildVerifyReq(
	res resolved, model, resumeID string, interactive bool,
	iter int, clarifyPath, agentLogPath string, mustRead []string,
) codingagents.VerifyRequest {
	return codingagents.VerifyRequest{
		TaskDir:                    res.TaskDir,
		RequirementsPath:           res.RequirementsPath,
		PlanPath:                   res.PlanPath,
		VerifierPlanOutputPath:     res.VerifierPlanPath,
		VerifierFindingsOutputPath: res.FindingsPath,
		ClarificationPath:          clarifyPath,
		Model:                      model,
		Interactive:                interactive,
		Resume:                     iter > 0,
		ResumeChatID:               resumeID,
		Worktree:                   res.Task.Worktree,
		AgentLogPath:               agentLogPath,
		MustRead:                   mustRead,
	}
}

// runVerifyTurn drives one verifier turn and blocks on its exit.
func runVerifyTurn(
	ctx context.Context, agent codingagents.Agent,
	req codingagents.VerifyRequest,
) error {
	pid, err := agent.Verify(ctx, req)
	if err != nil {
		return err
	}
	return run.WaitForExit(ctx, pid)
}

// runFixTurn drives one worker fix turn (resume + fix-findings) and
// blocks on its exit.
func runFixTurn(
	ctx context.Context, agent codingagents.Agent,
	interactive bool, res resolved,
	clarifyPath, agentLogPath string,
) error {
	req := codingagents.WorkRequest{
		TaskDir:                    res.TaskDir,
		PlanPath:                   res.PlanPath,
		Model:                      res.Task.WorkModel,
		ClarificationPath:          clarifyPath,
		Interactive:                interactive,
		ResumeChatID:               res.Task.WorkResumeSession,
		Resume:                     true,
		FixFindings:                true,
		VerifierFindingsOutputPath: res.FindingsPath,
		Worktree:                   res.Task.Worktree,
		AgentLogPath:               agentLogPath,
	}
	pid, err := agent.Work(ctx, req)
	if err != nil {
		return err
	}
	return run.WaitForExit(ctx, pid)
}
