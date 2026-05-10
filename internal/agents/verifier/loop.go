package verifier

import (
	"context"
	"path/filepath"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/util/run"
)

// buildVerifyRequest composes the per-iteration VerifyRequest.
func buildVerifyRequest(
	res resolver.VerifyTask,
	session codingagents.AgentSession,
	iter int,
	interactive bool,
	mustRead []string,
) codingagents.VerifyRequest {
	return codingagents.VerifyRequest{
		RequirementsPath:           res.Paths.Requirements,
		PlanPath:                   res.Paths.Plan,
		VerifierPlanOutputPath:     res.Paths.VerifierPlan,
		VerifierFindingsOutputPath: res.Paths.Findings,
		ClarificationPath:          res.Paths.Clarification,
		Model:                      session.Model,
		Interactive:                interactive,
		Resume:                     iter > 0,
		ResumeChatID:               session.ResumeID,
		Worktree:                   res.Task.Worktree,
		AgentLogPath: filepath.Join(
			res.TaskDir,
			tasks.AgentLogFileName,
		),
		MustRead: mustRead,
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
	ctx context.Context,
	agent codingagents.Agent,
	req codingagents.WorkRequest,
) error {
	pid, err := agent.Work(ctx, req)
	if err != nil {
		return err
	}
	return run.WaitForExit(ctx, pid)
}

func buildFixRequest(
	res resolver.VerifyTask,
	interactive bool,
) codingagents.WorkRequest {
	return codingagents.WorkRequest{
		PlanPath:                   res.Paths.Plan,
		Model:                      res.Task.WorkModel,
		ClarificationPath:          res.Paths.Clarification,
		Interactive:                interactive,
		ResumeChatID:               res.Task.WorkResumeSession,
		Resume:                     true,
		FixFindings:                true,
		VerifierFindingsOutputPath: res.Paths.Findings,
		Worktree:                   res.Task.Worktree,
		AgentLogPath: filepath.Join(
			res.TaskDir,
			tasks.AgentLogFileName,
		),
	}
}
