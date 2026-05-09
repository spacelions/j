package verifier

import (
	"context"
	"io"
	"time"

	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/util/run"
)

// buildVerifyReq composes the per-iteration VerifyRequest.
func buildVerifyReq(
	res resolved, model, resumeID string, interactive bool,
	iter int, clarifyPath, agentLogPath string, mustRead []string,
) codingagents.VerifyRequest {
	return codingagents.VerifyRequest{
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

// captureVerifyResume asks the verifier agent for the post-run
// session id (deepseek-tui mints it after the first turn writes to
// disk) and threads it onto lc + the loop's resumeID variable so
// subsequent iterations resume the same session. A scan failure
// surfaces as a best-effort warning. Returns the captured id (or
// "" when nothing matched) so the caller updates its local var.
func captureVerifyResume(
	ctx context.Context, stderr io.Writer,
	lc *lifecycle.VerifyLifecycle, agent codingagents.Agent,
	workspace string, since time.Time,
) string {
	id, err := codingagents.CaptureResumeID(
		ctx, agent, workspace, since,
	)
	if err != nil {
		uitheme.DangerousDialogBox(stderr, "J: %v", err)
		return ""
	}
	lc.RecordResumeSession(id)
	return id
}
