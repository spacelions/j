package plan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/cli/picker"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/util/mdfile"
	"github.com/spacelions/j/internal/util/run"
)

func allowedForReplan(t store.Task) bool {
	switch t.Status {
	case store.StatusPlanDone, store.StatusHelp:
		return true
	}
	return false
}

// confirmStatusOverride decides whether to run agent.Plan against a
// task whose status falls outside the allowlist. The allowlist
// returns true -> proceed silently. Otherwise --yes / PLAN_YES ->
// proceed silently; else delegate to the UI confirm prompt and
// return its bool. A user decline (false from the prompt) returns
// proceed=false with err=nil so the caller can exit cleanly.
func confirmStatusOverride(ctx context.Context, opts Options, cmd string, t store.Task, allowed func(store.Task) bool) (bool, error) {
	if allowed(t) {
		return true, nil
	}
	if opts.Yes {
		return true, nil
	}
	return opts.UI.ConfirmStatusOverride(ctx, cmd, t.ID, string(t.Status))
}

// runMarkdown is the markdown-file flow: resolve and read the source,
// pick a tool/model, mint a task ID, ensure `<cwd>/.j/tasks/<id>/`
// exists, and ask the agent to save both the (possibly refined)
// requirements.md and the produced plan.md inside that directory. The
// orchestrator reads both files after agent.Plan returns and updates
// the task summary accordingly. A `planning` task is logged before
// agent.Plan and updated to `plan-done` on success or `help` on
// failure.
func runMarkdown(ctx context.Context, opts Options, rawTarget string) error {
	target, err := mdfile.Resolve(rawTarget)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}

	agent, model, err := selectPlanner(ctx, opts)
	if err != nil {
		return err
	}

	taskID := store.NewTaskID()
	taskDir, err := store.EnsureTaskDir(taskID)
	if err != nil {
		return fmt.Errorf("plan: ensure task dir: %w", err)
	}
	requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
	planPath := filepath.Join(taskDir, store.PlanFileName)

	resumeID, err := agent.NewResumeID(ctx)
	if err != nil {
		banner.DangerousFprintf(opts.Stderr, "J: warning: %v\n", err)
	}
	agentLogPath := filepath.Join(taskDir, store.AgentLogFileName)
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		banner.DangerousFprintf(opts.Stderr, "J: warning: %v\n", mustReadErr)
	}
	lc := store.NewPlanTask(opts.Stderr, agent.Name(), model, taskID, target, string(body), resumeID)
	pid, planErr := agent.Plan(ctx, codingagents.PlanRequest{
		FromFilePath:           target,
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
				lc.Finish(err, "", "", target)
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
	lc.Finish(planErr, refinedReq, planMD, target)
	if planErr != nil {
		return planErr
	}

	banner.Fprintf(opts.Stdout, "J: the requirements.md and plan.md are saved in .j/tasks/%s/\n", taskID)
	return nil
}

// selectPlanner delegates to resolver.Agent with the planner bucket.
// Resolver owns the precedence chain (explicit -> stored -> prompt) and
// the persist step; the cli only forwards inputs.
func selectPlanner(ctx context.Context, opts Options) (codingagents.Agent, string, error) {
	return resolver.Agent(ctx, resolver.AgentOptions{
		Bucket:        store.BucketPlanner,
		Agents:        opts.Agents,
		ExplicitTool:  opts.Tool,
		ExplicitModel: opts.Model,
		UI:            opts.UI,
		Store:         opts.Store,
		Stderr:        opts.Stderr,
		Interactive:   opts.Interactive,
	})
}

func (o Options) withDefaults() Options {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.UI == nil {
		o.UI = picker.New(o.Stdin, o.Stderr)
	}
	return o
}
