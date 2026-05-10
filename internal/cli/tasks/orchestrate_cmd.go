package tasks

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/lifecycle/orchestrator"
)

// newOrchestrateCmd builds the hidden `j tasks orchestrate` cobra
// subcommand. It is hidden because users never invoke it directly:
// `j tasks start` forks a detached child that re-executes the j
// binary with this sub-command.
func newOrchestrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    cmdOrchestrate,
		Short:  "Drive planner → worker → verifier for a seeded task",
		Hidden: true,
		Long: "Internal command invoked by the detached child that " +
			"`j tasks start` spawns. Drives planner → worker → verifier " +
			"end to end against the seeded task identified by --id.",
		RunE: runOrchestrateCmd,
	}
	bindOrchestrateFlags(cmd)
	return cmd
}

func runOrchestrateCmd(cmd *cobra.Command, _ []string) error {
	approval, err := orchestratePlanRequiresApprovalOverride(cmd)
	if err != nil {
		return err
	}
	phase, err := orchestrator.ParseRunPhase(
		viper.GetString("tasks.orchestrate.phase"))
	if err != nil {
		return err
	}
	interactive := explicitBool(cmd, flagKeyInteractive,
		"tasks.orchestrate.interactive",
		"TASKS_ORCHESTRATE_INTERACTIVE")
	return RunOrchestrate(cmd.Context(), OrchestrateOptions{
		TaskID:               viper.GetString("tasks.orchestrate.id"),
		PlanRequiresApproval: approval,
		Phase:                phase,
		Tool:                 viper.GetString("tasks.orchestrate.tool"),
		Model:                viper.GetString("tasks.orchestrate.model"),
		Interactive:          interactive,
		Yes:                  viper.GetBool("tasks.orchestrate.yes"),
		Stdin:                cmd.InOrStdin(),
		Stdout:               cmd.OutOrStdout(),
		Stderr:               cmd.ErrOrStderr(),
		Agents:               defaultAgents(),
	})
}

func bindOrchestrateFlags(cmd *cobra.Command) {
	cmd.Flags().String("id", "",
		"Task id whose planner→worker→verifier chain to drive")
	cmd.Flags().Bool("plan-requires-approval", false,
		"Resolved project.plan_requires_approval value")
	cmd.Flags().String("phase", string(orchestrator.RunPhaseFull),
		"Which slice of the chain to run: full | plan-only | "+
			"from-work | work-only | verify-only")
	cmd.Flags().String(flagKeyTool, "", "Planner tool override (cursor|claude)")
	cmd.Flags().String(flagKeyModel, "", "Planner model override")
	cmd.Flags().Bool(flagKeyInteractive, false,
		"Run the active phase in TUI mode")
	cmd.Flags().Bool("yes", false,
		"Skip status-mismatch confirmation in the planner")
	bindFlagEnv(cmd,
		bindEnv("tasks.orchestrate.id", "id", "TASKS_ORCHESTRATE_ID"),
		bindEnv("tasks.orchestrate.plan_requires_approval",
			"plan-requires-approval",
			"TASKS_ORCHESTRATE_PLAN_REQUIRES_APPROVAL"),
		bindEnv("tasks.orchestrate.phase", "phase",
			"TASKS_ORCHESTRATE_PHASE"),
		bindEnv("tasks.orchestrate.tool", flagKeyTool,
			"TASKS_ORCHESTRATE_TOOL"),
		bindEnv("tasks.orchestrate.model", flagKeyModel,
			"TASKS_ORCHESTRATE_MODEL"),
		bindEnv("tasks.orchestrate.interactive", flagKeyInteractive,
			"TASKS_ORCHESTRATE_INTERACTIVE"),
		bindEnv("tasks.orchestrate.yes", "yes", "TASKS_ORCHESTRATE_YES"),
	)
}

func orchestratePlanRequiresApprovalOverride(
	cmd *cobra.Command,
) (*bool, error) {
	return explicitBoolPtr(cmd, "plan-requires-approval",
		"tasks.orchestrate.plan_requires_approval",
		"TASKS_ORCHESTRATE_PLAN_REQUIRES_APPROVAL"), nil
}
