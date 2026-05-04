package plan

import (
	"context"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/banner"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
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

	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		return err
	}
	taskDir := filepath.Join(tasksDir, existing.ID)
	requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
	planPath := filepath.Join(taskDir, store.PlanFileName)

	agent, model, err := selectPlanner(ctx, opts)
	if err != nil {
		return err
	}

	resumeID, err := agent.NewResumeID(ctx)
	if err != nil {
		banner.DangerousFprintf(opts.Stderr, "J: warning: %v\n", err)
	}
	agentLogPath := filepath.Join(taskDir, store.AgentLogFileName)
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		banner.DangerousFprintf(opts.Stderr, "J: warning: %v\n", mustReadErr)
	}
	lc := existing.BeginPlanReuse(opts.Stderr, agent.Name(), model, resumeID)
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
			banner.RunningInBackground(opts.Stdout, agent.Name(), pid, agentLogPath)
			return nil
		}
	}

	var refinedReq, planMD string
	if planErr == nil {
		if data, readErr := os.ReadFile(requirementsPath); readErr == nil {
			refinedReq = string(data)
		} else {
			banner.DangerousFprintf(opts.Stderr, "J: warning: read %s: %v\n", requirementsPath, readErr)
		}
		if data, readErr := os.ReadFile(planPath); readErr == nil {
			planMD = string(data)
		} else {
			banner.DangerousFprintf(opts.Stderr, "J: warning: read %s: %v\n", planPath, readErr)
		}
	}
	lc.Finish(planErr, refinedReq, planMD, requirementsPath)
	if planErr != nil {
		return planErr
	}

	banner.Fprintf(opts.Stdout, "J: re-planned task %s\n", existing.ID)
	return nil
}
