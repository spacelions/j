package tasks

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/cli/preflight"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
)

const noActiveVerifySessionMessage = "J: no tasks"

// ResumeVerifyOptions configures RunResumeVerify.
type ResumeVerifyOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI

	JBinary string
}

var resumeVerifyConfig = resumePhaseConfig{
	emptyMsg:   noActiveVerifySessionMessage,
	errorVerb:  "resume-verify",
	hasSession: func(t tasks.Task) bool { return t.VerifyResumeSession != "" },
	gate:       requirePlanAndPriorWork,
	orchestrateArgs: func(taskID string) []string {
		return []string{
			cmdTasks, cmdOrchestrate,
			flagID, taskID,
			flagPhaseVerifyOnly,
			flagInteractiveTrue,
		}
	},
}

// RunResumeVerify implements `j tasks resume-verify`.
func RunResumeVerify(ctx context.Context, opts ResumeVerifyOptions) error {
	return runResumePhase(ctx, resumeOptions{
		Stdin:   opts.Stdin,
		Stdout:  opts.Stdout,
		Stderr:  opts.Stderr,
		UI:      opts.UI,
		JBinary: opts.JBinary,
	}, resumeVerifyConfig)
}

// newResumeVerifyCmd builds the `j tasks resume-verify` cobra subcommand.
func newResumeVerifyCmd() *cobra.Command {
	agents := defaultAgents()
	cmd := &cobra.Command{
		Use:   "resume-verify",
		Short: "Resume verifier for a task in the foreground",
		Long: "Renders the shared task picker. The selected task re-execs " +
			"`j tasks orchestrate --phase=verify-only --interactive=true` " +
			"inline so the verifier runs in the foreground.",
		PersistentPreRunE: preflight.PreRunE,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return preflight.EnsureAgentSelections(
				cmd.Context(),
				preflight.AgentCheckOptions{
					Stdin:  cmd.InOrStdin(),
					Stdout: cmd.OutOrStdout(),
					Stderr: cmd.ErrOrStderr(),
					Agents: agents,
				})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunResumeVerify(cmd.Context(), ResumeVerifyOptions{
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: agents,
			})
		},
	}
	return cmd
}
