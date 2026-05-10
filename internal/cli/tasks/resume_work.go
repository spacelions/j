//nolint:dupl // intentionally parallel to resume_plan.go
package tasks

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/cli/preflight"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
)

// noActiveWorkSessionMessage is shown when no active work session exists.
const noActiveWorkSessionMessage = "J: no tasks with an active work session"

// ResumeWorkOptions configures RunResumeWork.
type ResumeWorkOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI

	JBinary string
}

var resumeWorkConfig = resumePhaseConfig{
	emptyMsg:    noActiveWorkSessionMessage,
	resumeEvent: tasks.EventWorkResume,
	errorVerb:   "resume-work",
	hasSession:  func(t tasks.Task) bool { return t.WorkResumeSession != "" },
	gate:        requirePlan,
	startStatus: tasks.StatusWorking,
	orchestrateArgs: func(taskID string) []string {
		return []string{
			cmdTasks, cmdOrchestrate,
			flagID, taskID,
			flagPhaseWorkOnly,
			flagInteractiveTrue,
		}
	},
}

// RunResumeWork implements `j tasks resume-work`.
func RunResumeWork(ctx context.Context, opts ResumeWorkOptions) error {
	return runResumePhase(ctx, resumeOptions{
		Stdin:   opts.Stdin,
		Stdout:  opts.Stdout,
		Stderr:  opts.Stderr,
		UI:      opts.UI,
		JBinary: opts.JBinary,
	}, resumeWorkConfig)
}

// newResumeWorkCmd builds the `j tasks resume-work` cobra subcommand.
func newResumeWorkCmd() *cobra.Command {
	agents := defaultAgents()
	cmd := &cobra.Command{
		Use: "resume-work",
		Short: "Resume an in-flight worker session in the foreground " +
			"with --interactive=true",
		Long: "Renders the shared task picker. The selected task re-execs " +
			"`j tasks orchestrate --phase=work-only --interactive=true` " +
			"inline so the worker resumes its session in the foreground with the " +
			"parent's terminal attached.",
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
			return RunResumeWork(cmd.Context(), ResumeWorkOptions{
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: agents,
			})
		},
	}
	return cmd
}
