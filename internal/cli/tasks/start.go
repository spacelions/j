package tasks

import (
	"context"
	"errors"
	"io"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/preflight"
	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/util/run"
)

// StartOptions configures RunStart. Stdin/Stdout/Stderr default to the
// process streams; Agents must be supplied by the caller (the cobra
// wiring injects every registered backend — cursor, claude,
// deepseek — tests inject scripted ones); UI defaults to picker.New
// so the source / file / re-plan pickers work.
type StartOptions struct {
	// FromFile is the markdown task description path. When set, the
	// source picker is skipped and the markdown branch fires directly.
	FromFile string
	// FromLinear is a Linear issue identifier. When set, RunStart
	// fetches the issue and stages requirements.md from the rendered
	// markdown without prompting.
	FromLinear string
	// FromTask, when set, resolves an existing task by ID and re-plans
	// it in place. Beats FromFile / FromLinear when all three are set.
	FromTask string

	// Tool and Model are one-off planner overrides forwarded into the
	// orchestrate argv. When non-empty the planner uses them instead
	// of (or to supplement) the stored bucket values.
	Tool  string
	Model string

	// Interactive, when non-nil, overrides the planner's interactive
	// flag. nil means inherit the stored bucket value.
	Interactive *bool

	// Yes skips the status-mismatch confirmation prompt when the
	// resolved task is not in plan-done / help.
	Yes bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	// UI drives the source / file / re-plan pickers when FromFile /
	// FromTask are empty. Defaults to picker.New.
	UI StartUI

	// JBinary is the absolute path to the j binary re-executed as
	// `j tasks orchestrate --id <id>`. Empty falls back to
	// os.Executable. Tests inject a path-resolvable stub.
	JBinary string

	// PlanRequiresApproval, when non-nil, overrides
	// project.plan_requires_approval for this start. nil means inherit
	// the project setting.
	PlanRequiresApproval *bool
}

type startTarget = resolver.StartTarget

func newStartCmd() *cobra.Command {
	agents := defaultAgents()
	cmd := &cobra.Command{
		Use: "start",
		Short: "Start a new task: drive planner, then pause for " +
			"approval or continue in the background",
		Long: "Validates that every agent bucket (planner, worker, " +
			"verifier) has a tool/model selection, then runs " +
			"`j tasks orchestrate --id <id>` either inline (with " +
			"--interactive=true so the TUI can render in the parent's " +
			"terminal) or as a detached child that the parent reports a PID " +
			"for and returns from. Pass --from-file/-f (or " +
			"TASKS_START_FROM_FILE) to point at a markdown description; " +
			"--from-task to re-plan an existing task; without either, the " +
			"source picker is rendered.",
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
			approval := startPlanRequiresApprovalOverride(cmd)
			interactive := explicitBoolPtr(cmd, flagKeyInteractive,
				"tasks.start.interactive", "TASKS_START_INTERACTIVE")
			return RunStart(cmd.Context(), StartOptions{
				FromFile:             viper.GetString("tasks.start.from_file"),
				FromLinear:           viper.GetString("tasks.start.from_linear"),
				FromTask:             viper.GetString("tasks.start.from_task"),
				Tool:                 viper.GetString("tasks.start.tool"),
				Model:                viper.GetString("tasks.start.model"),
				Interactive:          interactive,
				Yes:                  viper.GetBool("tasks.start.yes"),
				PlanRequiresApproval: approval,
				Stdin:                cmd.InOrStdin(),
				Stdout:               cmd.OutOrStdout(),
				Stderr:               cmd.ErrOrStderr(),
				Agents:               agents,
			})
		},
	}
	bindStartFlags(cmd)
	return cmd
}

// RunStart implements `j tasks start`. It mints (or re-uses) a task
// id, optionally stages the user's markdown into requirements.md, and
// seeds the bbolt task row at status `planning`. With
// `--interactive=true` it re-execs `j tasks orchestrate` inline so the
// TUI can render in the parent's terminal and blocks until the child
// exits. Without `--interactive` it forks a detached
// `j tasks orchestrate --id <id>` subprocess, records the child's PID
// on the row, prints the fork dialog, and returns immediately.
func RunStart(ctx context.Context, opts StartOptions) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("no coding agents configured")
	}

	target, err := resolveStartTarget(ctx, opts)
	if err != nil {
		return err
	}
	if target.TaskID == "" {
		// Linear source or aborted picker — exit cleanly.
		return nil
	}

	// Existing tasks need status confirmation before re-planning.
	if !target.IsNew {
		task, _ := resolver.TaskByID(target.TaskID)
		proceed, err := resolver.ConfirmStatusOverride(
			ctx, opts.UI, opts.Yes, "re-plan", task, resolver.ReplanAllowed)
		if err != nil {
			return err
		}
		if !proceed {
			return nil
		}
	}

	planRequiresApproval, _ := resolvePlanRequiresApproval(
		opts.PlanRequiresApproval)
	// Re-plan always runs plan-only so the user can review the updated
	// plan before work starts.
	if !target.IsNew {
		planRequiresApproval = true
	}

	agentLogPath, err := prepareTaskFiles(target)
	if err != nil {
		return err
	}

	interactive := resolver.Interactive(opts.Interactive)
	orchestrateArgs := buildOrchestrateArgs(
		target.TaskID, planRequiresApproval, interactive, opts,
	)

	if interactive {
		persistStartRow(opts.Stderr, target, "", 0)
		return runInlineOrchestrator(ctx, opts.JBinary, orchestrateArgs)
	}

	pid, err := spawnDetachedOrchestrator(
		ctx, opts.JBinary, agentLogPath, orchestrateArgs)
	if err != nil {
		return err
	}
	persistStartRow(opts.Stderr, target, agentLogPath, pid)
	uitheme.NormalForkDialog(opts.Stdout,
		"task "+target.TaskID, pid, agentLogPath)
	return nil
}

// buildOrchestrateArgs assembles the argv passed to
// `j tasks orchestrate`. Extracted from RunStart so that function
// stays under the 80-line method cap.
func buildOrchestrateArgs(
	taskID string, planRequiresApproval, interactive bool, opts StartOptions,
) []string {
	args := []string{
		cmdTasks, cmdOrchestrate,
		flagID, taskID,
		"--plan-requires-approval=" +
			strconv.FormatBool(planRequiresApproval),
		"--interactive=" + strconv.FormatBool(interactive),
	}
	if opts.Tool != "" {
		args = append(args, "--tool="+opts.Tool)
	}
	if opts.Model != "" {
		args = append(args, "--model="+opts.Model)
	}
	if opts.Yes {
		args = append(args, "--yes")
	}
	return args
}

// spawnDetachedOrchestrator resolves the j binary, opens / re-uses
// the per-task agent.log via run.SpawnIn, and returns the spawned
// child's PID. Shared between `j tasks start` (planner-first spawn)
// and `j tasks continue` (resume-after-plan-done spawn).
func spawnDetachedOrchestrator(
	ctx context.Context, binaryOverride, agentLogPath string, args []string,
) (int, error) {
	binary, _ := resolveJBinary(binaryOverride)
	return run.SpawnIn(ctx, "", agentLogPath, binary, args...)
}

// runInlineOrchestrator resolves the j binary and re-execs it inline
// (blocking, parent's stdin/stdout/stderr inherited) so a TUI can
// render. Used by the `--interactive=true` paths of `j tasks start`
// / `re-plan` and unconditionally by `j tasks resume-plan`.
func runInlineOrchestrator(
	ctx context.Context, binaryOverride string, args []string,
) error {
	binary, _ := resolveJBinary(binaryOverride)
	return run.RunIn(ctx, "", binary, args...)
}

func resolveStartTarget(
	ctx context.Context, opts StartOptions,
) (startTarget, error) {
	if opts.FromTask != "" {
		return resolver.StartTargetFromExistingTask(ctx, opts.FromTask)
	}
	if opts.FromFile != "" {
		return resolver.NewStartTargetFromMarkdown(opts.FromFile)
	}
	if opts.FromLinear != "" {
		return resolver.StartTargetFromLinear(ctx, opts.FromLinear)
	}
	return resolver.ResolveStartTarget(ctx, opts.UI, opts.Stdout, "")
}

// prepareTaskFiles ensures the per-task directory exists and, for
// new tasks, stages requirements.md from the in-memory body.
// Returns the absolute path to the per-task agent.log so the caller
// can hand it to run.SpawnIn. For re-plan targets, requirements.md
// is left untouched — the resolver helper already handled it.
func prepareTaskFiles(target startTarget) (string, error) {
	return resolver.PrepareStartTaskFiles(target)
}
