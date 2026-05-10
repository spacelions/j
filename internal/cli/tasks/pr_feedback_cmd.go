package tasks

import (
	"github.com/spf13/cobra"
)

// newPRFeedbackCmd builds the manual PR command processor. V1 takes
// one JSON payload path so callers can supply GitHub context without
// a webhook, tunnel, poller, or watcher.
func newPRFeedbackCmd() *cobra.Command {
	opts := PRFeedbackOptions{}
	cmd := &cobra.Command{
		Use:   "pr-feedback",
		Short: "Process one manual GitHub PR feedback command",
		Long: "Processes one explicit GitHub PR command payload. " +
			"Only @j take a look from the PR author is accepted, and " +
			"accepted commands run planner-only feedback triage.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.Stdout = cmd.OutOrStdout()
			opts.Stderr = cmd.ErrOrStderr()
			opts.Agents = defaultAgents()
			return RunPRFeedback(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.InputPath, "input", "",
		"JSON file carrying the PR command payload")
	cmd.Flags().StringVar(&opts.Tool, flagKeyTool, "",
		"Planner tool override (cursor|claude|codex)")
	cmd.Flags().StringVar(&opts.Model, flagKeyModel, "",
		"Planner model override")
	cmd.Flags().BoolVar(&opts.Interactive, flagKeyInteractive, false,
		"Run the planner in interactive mode")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}
