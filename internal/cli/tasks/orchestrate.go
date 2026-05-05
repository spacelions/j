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
	"github.com/spacelions/j/internal/util/agentlog"
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

	// PlanRequiresApproval, when non-nil, is the resolved gate value
	// passed by `j tasks start`. nil makes direct/internal callers
	// inherit project.plan_requires_approval.
	PlanRequiresApproval *bool

	// SkipPlanning, when true, runs only worker → verifier on a
	// task already past the planner. Set by `j tasks continue` when
	// it picks up a `plan-done` row. Mutually exclusive with
	// PlanRequiresApproval=true.
	SkipPlanning bool

	// Tool and Model are one-off planner overrides forwarded from
	// `j tasks start --tool/--model`.
	Tool  string
	Model string

	// Interactive controls whether the planner runs in TUI mode.
	// Defaults to false (headless). Set by `j tasks start --interactive`.
	Interactive bool

	// Yes skips status-mismatch confirmation in the planner.
	Yes bool

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
// plus `project.plan_requires_approval` — `project.api_key` /
// `project.model` are NOT required on this path because the shell-out
// branch never instantiates a Gemini model), then drives planner only
// or planner → worker → verifier (or worker → verifier when
// SkipPlanning is set) via the matching workflow.RunForTask* entry
// point. The agent.log redirection is the parent's concern: the
// caller opens the per-task log with O_APPEND and passes its fd as
// our stdout/stderr, so any line the chain writes (including warnings
// from this function) lands chronologically.
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
	planRequiresApproval, err := resolvePlanRequiresApproval(opts.PlanRequiresApproval)
	if err != nil {
		return err
	}
	overrides := workflow.PlannerOverrides{
		Tool:        opts.Tool,
		Model:       opts.Model,
		Interactive: opts.Interactive,
		Yes:         opts.Yes,
	}
	emitSessionStart(opts.Stderr, opts.TaskID, opts.SkipPlanning)
	if opts.SkipPlanning {
		if planRequiresApproval {
			return errors.New("tasks: --skip-planning is incompatible with --plan-requires-approval=true")
		}
		return workflow.RunForTaskFromWork(ctx, cfg, opts.TaskID, opts.Agents, opts.Stderr)
	}
	return workflow.RunForTaskWithGate(ctx, cfg, opts.TaskID, opts.Agents, opts.Stderr, planRequiresApproval, overrides)
}

// emitSessionStart writes one `session_start` marker into the agent
// log at the very top of orchestrate so a tailer can pin the task id,
// orchestrator pid, working directory, and skip-planning flag without
// reading bbolt. Field collection is deliberately cheap — os.Hostname
// and os.Getwd never block — and write errors are swallowed because
// markers are observability signal, not load-bearing data.
func emitSessionStart(w io.Writer, taskID string, skipPlanning bool) {
	hostname, _ := os.Hostname()
	cwd, _ := os.Getwd()
	_ = agentlog.Emit(w, "session_start", map[string]any{
		"task":             taskID,
		"orchestrator_pid": os.Getpid(),
		"hostname":         hostname,
		"cwd":              cwd,
		"skip_planning":    skipPlanning,
	})
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
// it. The flag surface is `--id` plus the resolved plan-approval gate
// (both with env bindings for completeness).
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
			approval, err := orchestratePlanRequiresApprovalOverride(cmd)
			if err != nil {
				return err
			}
			var interactive bool
			if cmd.Flags().Changed("interactive") || envSet("TASKS_ORCHESTRATE_INTERACTIVE") {
				interactive = viper.GetBool("tasks.orchestrate.interactive")
			}
			return RunOrchestrate(cmd.Context(), OrchestrateOptions{
				TaskID:               viper.GetString("tasks.orchestrate.id"),
				PlanRequiresApproval: approval,
				SkipPlanning:         viper.GetBool("tasks.orchestrate.skip_planning"),
				Tool:                 viper.GetString("tasks.orchestrate.tool"),
				Model:                viper.GetString("tasks.orchestrate.model"),
				Interactive:          interactive,
				Yes:                  viper.GetBool("tasks.orchestrate.yes"),
				Stdin:                cmd.InOrStdin(),
				Stdout:               cmd.OutOrStdout(),
				Stderr:               cmd.ErrOrStderr(),
				Agents:               []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().String("id", "", "Task id whose planner→worker→verifier chain to drive")
	cmd.Flags().Bool("plan-requires-approval", false, "Resolved project.plan_requires_approval value")
	cmd.Flags().Bool("skip-planning", false, "Run only worker → verifier on a task already past the planner")
	cmd.Flags().String("tool", "", "Planner tool override (cursor|claude)")
	cmd.Flags().String("model", "", "Planner model override")
	cmd.Flags().Bool("interactive", false, "Run planner in interactive (TUI) mode")
	cmd.Flags().Bool("yes", false, "Skip status-mismatch confirmation in the planner")
	_ = viper.BindPFlag("tasks.orchestrate.id", cmd.Flags().Lookup("id"))
	_ = viper.BindEnv("tasks.orchestrate.id", "TASKS_ORCHESTRATE_ID")
	_ = viper.BindPFlag("tasks.orchestrate.plan_requires_approval", cmd.Flags().Lookup("plan-requires-approval"))
	_ = viper.BindEnv("tasks.orchestrate.plan_requires_approval", "TASKS_ORCHESTRATE_PLAN_REQUIRES_APPROVAL")
	_ = viper.BindPFlag("tasks.orchestrate.skip_planning", cmd.Flags().Lookup("skip-planning"))
	_ = viper.BindEnv("tasks.orchestrate.skip_planning", "TASKS_ORCHESTRATE_SKIP_PLANNING")
	_ = viper.BindPFlag("tasks.orchestrate.tool", cmd.Flags().Lookup("tool"))
	_ = viper.BindEnv("tasks.orchestrate.tool", "TASKS_ORCHESTRATE_TOOL")
	_ = viper.BindPFlag("tasks.orchestrate.model", cmd.Flags().Lookup("model"))
	_ = viper.BindEnv("tasks.orchestrate.model", "TASKS_ORCHESTRATE_MODEL")
	_ = viper.BindPFlag("tasks.orchestrate.interactive", cmd.Flags().Lookup("interactive"))
	_ = viper.BindEnv("tasks.orchestrate.interactive", "TASKS_ORCHESTRATE_INTERACTIVE")
	_ = viper.BindPFlag("tasks.orchestrate.yes", cmd.Flags().Lookup("yes"))
	_ = viper.BindEnv("tasks.orchestrate.yes", "TASKS_ORCHESTRATE_YES")
	return cmd
}

func orchestratePlanRequiresApprovalOverride(cmd *cobra.Command) (*bool, error) {
	if cmd.Flags().Changed("plan-requires-approval") || envSet("TASKS_ORCHESTRATE_PLAN_REQUIRES_APPROVAL") {
		v := viper.GetBool("tasks.orchestrate.plan_requires_approval")
		return &v, nil
	}
	return nil, nil
}
