package plan

import (
	"context"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/util/run"
)

// runReplanTask is the re-plan flow: load the existing task row,
// confirm status if necessary, read the existing requirements.md,
// pick a tool/model, and re-run agent.Plan against the same task
// directory so requirements.md and plan.md are refreshed in
// place. The task row is mutated (status: planning → plan-done,
// preserving original PlanBeginAt).
func runReplanTask(ctx context.Context, opts Options, id string) error {
	existing, err := loadTaskByID(id)
	if err != nil {
		return err
	}
	proceed, err := resolver.ConfirmStatusOverride(ctx, opts.UI, opts.Yes, "re-plan", existing, resolver.ReplanAllowed)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		return err
	}
	taskDir := filepath.Join(tasksDir, existing.ID)
	requirementsPath := filepath.Join(taskDir, tasks.RequirementsFileName)
	planPath := filepath.Join(taskDir, tasks.PlanFileName)

	// Linear-sourced tasks: refresh requirements.md from the live
	// Linear issue before re-planning so the agent sees any title /
	// description edits made on the Linear side since the original
	// run. Markdown-sourced tasks keep their existing requirements.md.
	if existing.LinearIssue != "" {
		body, _, fetchErr := resolver.FetchLinearBody(ctx, existing.LinearIssue)
		if fetchErr != nil {
			return fetchErr
		}
		if writeErr := os.WriteFile(requirementsPath, []byte(body), 0o644); writeErr != nil {
			return writeErr
		}
	}

	agent, model, err := selectPlanner(ctx, opts)
	if err != nil {
		return err
	}

	resumeID, err := agent.NewResumeID(ctx)
	if err != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", err)
	}
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", mustReadErr)
	}
	lc := existing.BeginPlanReuse(opts.Stderr, agent.Name(), model, resumeID, agentLogPath)
	pid, planErr := agent.Plan(ctx, codingagents.PlanRequest{
		FromFilePath:           requirementsPath,
		Model:                  model,
		RequirementsOutputPath: requirementsPath,
		PlanOutputPath:         planPath,
		Interactive:            opts.Interactive,
		ResumeChatID:           resumeID,
		AgentLogPath:           agentLogPath,
		MustRead:               mustReadFiles,
	})

	if planErr == nil && pid > 0 {
		if opts.WaitForCompletion {
			if err := run.WaitForExit(ctx, pid); err != nil {
				lc.Finish(err, "", "", requirementsPath)
				return err
			}
		} else {
			lc.RecordBackground(pid, agentLogPath)
			uitheme.NormalForkDialog(opts.Stdout, agent.Name(), pid, agentLogPath)
			return nil
		}
	}

	var refinedReq, planMD string
	if planErr == nil {
		if data, readErr := os.ReadFile(requirementsPath); readErr == nil {
			refinedReq = string(data)
		} else {
			uitheme.DangerousDialogBox(opts.Stderr, "J: read %s: %v", requirementsPath, readErr)
		}
		if data, readErr := os.ReadFile(planPath); readErr == nil {
			planMD = string(data)
		} else {
			uitheme.DangerousDialogBox(opts.Stderr, "J: read %s: %v", planPath, readErr)
		}
	}
	lc.Finish(planErr, refinedReq, planMD, requirementsPath)
	if planErr != nil {
		return planErr
	}

	uitheme.NormalFprintf(opts.Stdout, "J: re-planned task %s\n", existing.ID)
	return nil
}
