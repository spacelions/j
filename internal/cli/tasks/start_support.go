package tasks

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/preflight"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/store"
)

func persistStartRow(stderr io.Writer, target startTarget, agentLogPath string, pid int) {
	if target.isNew {
		begin := time.Now().UTC()
		store.PersistWarn(stderr, store.Task{
			ID:            target.taskID,
			Status:        store.StatusPlanning,
			Summary:       store.Summary(target.body, target.source),
			PlanBeginAt:   &begin,
			AgentLogPath:  agentLogPath,
			BackgroundPID: pid,
		})
		return
	}
	stampSpawnOnRow(stderr, target.taskID, agentLogPath, pid)
}

func resolveJBinary(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("J: resolve j binary: %w", err)
	}
	return exe, nil
}

func resolvePlanRequiresApproval(override *bool) (bool, error) {
	if override != nil {
		return *override, nil
	}
	return store.LoadPlanRequiresApproval()
}

func (o StartOptions) withDefaults() StartOptions {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.Selector == nil {
		o.Selector = picker.New(o.Stdin, o.Stderr)
	}
	if o.UI == nil {
		o.UI = picker.New(o.Stdin, o.Stderr)
	}
	return o
}

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new task: drive planner, then pause for approval or continue in the background",
		Long: "Validates that every agent bucket (planner, worker, verifier) " +
			"has a tool/model selection — prompting once per missing bucket — " +
			"then forks a detached `j tasks orchestrate --id <id>` child that " +
			"runs the planner and, when plan approval is not required, drives " +
			"worker → verifier end to end before exiting. Pass " +
			"--from-file/-f (or TASKS_START_FROM_FILE) to point at a markdown " +
			"task description; when neither is set, the same source picker " +
			"`j plan` shows is rendered (markdown | linear | existing " +
			"task). After the spawn, a bordered two-line banner is " +
			"printed (subject + PID on row one, `tail -f <agent.log>` on row " +
			"three) so the user can follow along. Every line written by the " +
			"orchestrator and the per-phase coding-agent children appends to " +
			"the same per-task <cwd>/.j/tasks/<id>/agent.log.",
		PersistentPreRunE: preflight.PreRunE,
		RunE: func(cmd *cobra.Command, _ []string) error {
			approval, err := startPlanRequiresApprovalOverride(cmd)
			if err != nil {
				return err
			}
			return RunStart(cmd.Context(), StartOptions{
				FromFile:             viper.GetString("tasks.start.from_file"),
				PlanRequiresApproval: approval,
				Stdin:                cmd.InOrStdin(),
				Stdout:               cmd.OutOrStdout(),
				Stderr:               cmd.ErrOrStderr(),
				Agents:               []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().StringP("from-file", "f", "", "Path to a markdown file describing the task")
	cmd.Flags().Bool("plan-requires-approval", false, "Override project.plan_requires_approval for this run (use =false to skip once)")
	_ = viper.BindPFlag("tasks.start.from_file", cmd.Flags().Lookup("from-file"))
	_ = viper.BindEnv("tasks.start.from_file", "TASKS_START_FROM_FILE")
	_ = viper.BindPFlag("tasks.start.plan_requires_approval", cmd.Flags().Lookup("plan-requires-approval"))
	_ = viper.BindEnv("tasks.start.plan_requires_approval", "TASKS_START_PLAN_REQUIRES_APPROVAL")
	return cmd
}

func startPlanRequiresApprovalOverride(cmd *cobra.Command) (*bool, error) {
	approvalSet := cmd.Flags().Changed("plan-requires-approval") || envSet("TASKS_START_PLAN_REQUIRES_APPROVAL")
	if approvalSet {
		v := viper.GetBool("tasks.start.plan_requires_approval")
		return &v, nil
	}
	return nil, nil
}

func envSet(name string) bool {
	_, ok := os.LookupEnv(name)
	return ok
}
