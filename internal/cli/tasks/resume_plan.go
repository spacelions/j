//nolint:dupl // intentionally parallel to resume_work.go
package tasks

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/cli/preflight"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/coding-agents/deepseek"
	"github.com/spacelions/j/internal/store/tasks"
)

// noActivePlanSessionMessage is shown when no active plan session exists.
const noActivePlanSessionMessage = "J: no tasks with an active plan session"

// ResumePlanOptions configures RunResumePlan.
type ResumePlanOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI

	// JBinary is the absolute path to the j binary. Empty falls back to
	// os.Executable. Tests inject a path-resolvable stub.
	JBinary string
}

var resumePlanConfig = resumePhaseConfig{
	emptyMsg:    noActivePlanSessionMessage,
	resumeEvent: tasks.EventPlanResume,
	errorVerb:   "resume-plan",
	hasSession:  func(t tasks.Task) bool { return t.PlanResumeSession != "" },
	orchestrateArgs: func(taskID string) []string {
		return []string{
			cmdTasks, cmdOrchestrate,
			flagID, taskID,
			flagPlanRequiresApprovalTrue,
			flagInteractiveTrue,
		}
	},
}

// RunResumePlan implements `j tasks resume-plan`.
func RunResumePlan(ctx context.Context, opts ResumePlanOptions) error {
	return runResumePhase(ctx, resumeOptions{
		Stdin:   opts.Stdin,
		Stdout:  opts.Stdout,
		Stderr:  opts.Stderr,
		UI:      opts.UI,
		JBinary: opts.JBinary,
	}, resumePlanConfig)
}

// newResumePlanCmd builds the `j tasks resume-plan` cobra subcommand.
func newResumePlanCmd() *cobra.Command {
	agents := []codingagents.Agent{cursor.New(), claude.New(), deepseek.New()}
	cmd := &cobra.Command{
		Use: "resume-plan",
		Short: "Resume an in-flight planner session in the foreground " +
			"with --interactive=true",
		Long: "Filters tasks to rows with a non-empty plan_resume_session and " +
			"renders the shared task picker. The selected task re-execs " +
			"`j tasks orchestrate --plan-requires-approval=true --interactive=true` " +
			"inline so the planner resumes its session in the foreground with the " +
			"parent's terminal attached. When no task carries an active plan session, " +
			"prints `J: no tasks with an active plan session` and exits 0.",
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
			return RunResumePlan(cmd.Context(), ResumePlanOptions{
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: agents,
			})
		},
	}
	return cmd
}
