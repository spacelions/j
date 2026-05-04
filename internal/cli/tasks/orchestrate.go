package tasks

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/workflow"
)

// OrchestrateOptions configures RunOrchestrate. The detached child
// spawned by `j tasks start` re-invokes itself as
// `j tasks orchestrate --id <id>`; this struct lets tests drive the
// same entry point with stub coding agents.
type OrchestrateOptions struct {
	// TaskID names the seeded task whose planner → worker → verifier
	// chain this invocation drives end to end. Required.
	TaskID string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// Agents is the wired coding-agent set; defaults are
	// `[]codingagents.Agent{cursor.New(), claude.New()}` when the
	// cobra wiring fires (tests inject scripted ones).
	Agents []codingagents.Agent
}

// RunOrchestrate is the body of `j tasks orchestrate --id <id>`. It
// reads the relaxed per-project task config (`project.max_iterations`
// only — `project.api_key` / `project.model` are NOT required on
// this path because the shell-out branch never instantiates a
// Gemini model), then drives planner → worker → verifier via
// workflow.RunForTask. The agent.log redirection is the parent's
// concern: `j tasks start` opens the per-task log with O_APPEND and
// passes its fd as our stdout/stderr, so any line the chain writes
// (including warnings from this function) lands chronologically.
func RunOrchestrate(ctx context.Context, opts OrchestrateOptions) error {
	opts = opts.withDefaults()
	if opts.TaskID == "" {
		return errors.New("tasks: orchestrate requires --id")
	}
	if len(opts.Agents) == 0 {
		return errors.New("tasks: no coding agents configured")
	}
	cfg, err := store.LoadTaskConfig()
	if err != nil {
		return err
	}
	return workflow.RunForTask(ctx, cfg, opts.TaskID, opts.Agents, opts.Stderr)
}

func (o OrchestrateOptions) withDefaults() OrchestrateOptions {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	return o
}

// newOrchestrateCmd builds the hidden `j tasks orchestrate` cobra
// subcommand. It is hidden because users never invoke it directly:
// `j tasks start` forks a detached child that re-executes the j
// binary with this sub-command, so help output should not advertise
// it. The flag surface is just `--id` (plus an env binding for
// completeness).
func newOrchestrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "orchestrate",
		Short:  "Drive planner → worker → verifier for a seeded task (internal)",
		Hidden: true,
		Long: "Internal command invoked by the detached child that " +
			"`j tasks start` spawns. Drives planner → worker → verifier " +
			"end to end against the seeded task identified by --id.",
		// No PersistentPreRunE: the detached child has no terminal,
		// and the parent's `j tasks start` already ran preflight.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunOrchestrate(cmd.Context(), OrchestrateOptions{
				TaskID: viper.GetString("tasks.orchestrate.id"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().String("id", "", "Task id whose planner→worker→verifier chain to drive")
	_ = viper.BindPFlag("tasks.orchestrate.id", cmd.Flags().Lookup("id"))
	_ = viper.BindEnv("tasks.orchestrate.id", "TASKS_ORCHESTRATE_ID")
	return cmd
}

