package verifier

import (
	"context"
	"path/filepath"
	"time"

	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
)

// runVerifyLoop alternates verifier turns with worker-resume fix
// turns until VERDICT: PASS or MaxIterations is exhausted.
// run.WaitForExit blocks on every spawned child so a headless
// backend's deferred write doesn't race the verdict parse.
func runVerifyLoop(
	ctx context.Context,
	agent codingagents.Agent,
	lc *lifecycle.VerifyLifecycle,
	res resolver.VerifyTask,
	session codingagents.AgentSession,
	opts Options,
) (lifecycle.VerifyOutcome, error) {
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", mustReadErr)
	}
	beginAt := time.Now().UTC()
	for i := range opts.MaxIterations {
		outcome, err := runVerifyIteration(
			ctx, agent, lc, res, &session, verifyIteration{
				index:         i,
				mustReadFiles: mustReadFiles,
				beginAt:       beginAt,
				opts:          opts,
			},
		)
		if err != nil || outcome == lifecycle.VerifyOutcomeSuccess {
			return outcome, err
		}
	}
	return lifecycle.VerifyOutcomeNoRetries, nil
}

type verifyIteration struct {
	index         int
	mustReadFiles []string
	beginAt       time.Time
	opts          Options
}

func runVerifyIteration(
	ctx context.Context,
	agent codingagents.Agent,
	lc *lifecycle.VerifyLifecycle,
	res resolver.VerifyTask,
	session *codingagents.AgentSession,
	iter verifyIteration,
) (lifecycle.VerifyOutcome, error) {
	lc.IterationBegin(iter.index, iter.opts.MaxIterations)
	req := buildVerifyRequest(
		res, *session, iter.index, iter.opts.Interactive, iter.mustReadFiles,
	)
	pid, err := startVerifyTurn(ctx, agent, req)
	if err != nil {
		return lifecycle.VerifyOutcomeNoRetries, err
	}
	capture := codingagents.ResumeCapture{
		TaskDir: res.TaskDir,
		Since:   iter.beginAt,
		Stderr:  iter.opts.Stderr,
	}
	if iter.index == 0 {
		resumeID, err := codingagents.CaptureAndSaveProcessResumeID(
			ctx, agent, lc, capture, codingagents.ResumeProcess{
				PID:      pid,
				Wait:     true,
				ResumeID: session.ResumeID,
			},
		)
		session.ResumeID = resumeID
		if err != nil {
			return lifecycle.VerifyOutcomeNoRetries, err
		}
	} else if err := codingagents.WaitForResumeProcess(ctx, pid); err != nil {
		return lifecycle.VerifyOutcomeNoRetries, err
	}
	return finishVerifyIteration(ctx, lc, res, iter)
}

func finishVerifyIteration(
	ctx context.Context,
	lc *lifecycle.VerifyLifecycle,
	res resolver.VerifyTask,
	iter verifyIteration,
) (lifecycle.VerifyOutcome, error) {
	verdict := resolver.ParseVerdict(res.Paths.Findings)
	lc.Verdict(iter.index, verdict, res.Paths.Findings)
	if verdict == resolver.VerdictPass {
		return lifecycle.VerifyOutcomeSuccess, nil
	}
	if iter.index+1 >= iter.opts.MaxIterations {
		return lifecycle.VerifyOutcomeNoRetries, nil
	}
	workerAgent, err := resolveFixAgent(iter.opts.Agents, res.Task)
	if err != nil {
		return lifecycle.VerifyOutcomeNoRetries, err
	}
	err = runFixTurn(ctx, workerAgent,
		buildFixRequest(res, iter.opts.Interactive))
	return lifecycle.VerifyOutcomeNoRetries, err
}

// buildVerifyRequest composes the per-iteration VerifyRequest.
func buildVerifyRequest(
	res resolver.VerifyTask,
	session codingagents.AgentSession,
	iter int,
	interactive bool,
	mustRead []string,
) codingagents.VerifyRequest {
	return codingagents.VerifyRequest{
		TaskDir:                    res.TaskDir,
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

// startVerifyTurn drives one verifier turn and returns its process id.
func startVerifyTurn(
	ctx context.Context, agent codingagents.Agent,
	req codingagents.VerifyRequest,
) (int, error) {
	return agent.Verify(ctx, req)
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
	return codingagents.WaitForResumeProcess(ctx, pid)
}

func buildFixRequest(
	res resolver.VerifyTask,
	interactive bool,
) codingagents.WorkRequest {
	return codingagents.WorkRequest{
		TaskDir:                    res.TaskDir,
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
