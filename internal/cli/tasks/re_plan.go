package tasks

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/preflight"
	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
)

// RePlanUI is the slice of picker methods RunRePlan drives: the
// shared task picker (when --from-task is empty) and the status-
// override confirm leaf (when the resolved task is in a status
// outside the re-plan allowlist). *huhUI satisfies it; tests inject
// a scripted fake.
type RePlanUI interface {
	PickTask(ctx context.Context, ts []tasks.Task) (string, bool, error)
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
}

// RePlanOptions configures RunRePlan. Stdin/Stdout/Stderr default to
// the process streams; UI defaults to the huh-backed implementation;
// Agents must be supplied by the caller (the cobra wiring injects
// `[]codingagents.Agent{cursor.New(), claude.New()}`, tests inject
// scripted ones).
type RePlanOptions struct {
	// FromTask, when set, resolves the task by ID and skips the
	// picker. Empty triggers the shared task picker over every row.
	FromTask string

	// Tool and Model are one-off planner overrides forwarded into the
	// orchestrate argv. Empty means "inherit the stored bucket value".
	Tool  string
	Model string

	// Interactive, when non-nil, overrides the planner's interactive
	// flag. nil means inherit the stored bucket value.
	Interactive *bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     RePlanUI

	// JBinary is the absolute path to the j binary re-executed as
	// `j tasks orchestrate --id <id>`. Empty falls back to
	// os.Executable. Tests inject a path-resolvable stub.
	JBinary string
}

func (o RePlanOptions) withDefaults() RePlanOptions {
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
		o.UI = newHuhUI(o.Stdin, o.Stderr)
	}
	return o
}

// RunRePlan implements `j tasks re-plan`. It resolves a task (via
// --from-task or the shared picker) and prompts for confirmation when
// the status is outside the re-plan allowlist. With
// `--interactive=true` it re-execs `j tasks orchestrate` inline so the
// TUI can render and blocks until the child exits. Without
// `--interactive` it forks a detached `j tasks orchestrate --id <id>
// --plan-requires-approval=true` child so the planner re-runs without
// the user waiting in-process.
func RunRePlan(ctx context.Context, opts RePlanOptions) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()

	taskID, ok, err := resolveRePlanTaskID(ctx, opts)
	if err != nil || !ok {
		return err
	}
	task, err := resolver.TaskByID(taskID)
	if err != nil {
		return err
	}
	proceed, err := resolver.ConfirmStatusOverride(ctx, opts.UI, false, "re-plan", task, resolver.ReplanAllowed)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	taskDir, err := tasks.EnsureDir(task.ID)
	if err != nil {
		return fmt.Errorf("J: ensure task dir: %w", err)
	}
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)

	args := []string{
		"tasks", "orchestrate",
		"--id", task.ID,
		"--plan-requires-approval=true",
	}
	if opts.Tool != "" {
		args = append(args, "--tool="+opts.Tool)
	}
	if opts.Model != "" {
		args = append(args, "--model="+opts.Model)
	}
	if opts.Interactive != nil {
		args = append(args, "--interactive="+strconv.FormatBool(*opts.Interactive))
	}

	if opts.Interactive != nil && *opts.Interactive {
		stampSpawnOnRow(opts.Stderr, task.ID, "", 0)
		return runInlineOrchestrator(ctx, opts.JBinary, args)
	}

	pid, err := spawnDetachedOrchestrator(ctx, opts.JBinary, agentLogPath, args)
	if err != nil {
		return err
	}
	stampSpawnOnRow(opts.Stderr, task.ID, agentLogPath, pid)
	uitheme.NormalForkDialog(opts.Stdout, fmt.Sprintf("task %s", task.ID), pid, agentLogPath)
	return nil
}

// resolveRePlanTaskID returns either the --from-task id (verified to
// exist) or the picker's selection. ok=false collapses both the
// empty-store short-circuit (emptyMessage already printed) and the
// picker user-abort so callers can return nil cleanly.
func resolveRePlanTaskID(ctx context.Context, opts RePlanOptions) (string, bool, error) {
	if opts.FromTask != "" {
		return opts.FromTask, true, nil
	}
	s, err := tasks.OpenDefault()
	if err != nil {
		return "", false, err
	}
	id, ok, err := pickRePlanFromStore(ctx, s, opts)
	_ = s.Close()
	return id, ok, err
}

func pickRePlanFromStore(ctx context.Context, s *tasks.Store, opts RePlanOptions) (string, bool, error) {
	rows, err := s.ListTasks()
	if err != nil {
		return "", false, err
	}
	if len(rows) == 0 {
		uitheme.NormalFprintln(opts.Stdout, emptyMessage)
		return "", false, nil
	}
	tasks.SortTasks(rows)
	return opts.UI.PickTask(ctx, rows)
}

// newRePlanCmd builds the `j tasks re-plan` cobra subcommand. The
// flag surface mirrors the planner-only knobs of `j tasks start`
// (--from-task / --tool / --model / --interactive). Without
// --from-task the picker fires; the status-override confirm prompt
// fires for every row outside the re-plan allowlist (plan-done /
// help). viper.BindPFlag / viper.BindEnv only fail on programmer
// errors so their returned errors are intentionally discarded.
func newRePlanCmd() *cobra.Command {
	agents := []codingagents.Agent{cursor.New(), claude.New()}
	cmd := &cobra.Command{
		Use:   "re-plan",
		Short: "Re-plan an existing task: run the planner inline (--interactive) or detached",
		Long: "Resolves a task (via --from-task or the shared picker) and either " +
			"re-execs `j tasks orchestrate --plan-requires-approval=true` inline " +
			"(with --interactive=true so the TUI can render in the parent's terminal) " +
			"or forks it as a detached child so the planner re-runs without the user " +
			"waiting in-process. Tasks in plan-done or help skip the status-override " +
			"prompt; any other status renders a yes/no confirm before the orchestrator " +
			"runs. --tool / --model / --interactive forward into the orchestrate argv " +
			"as one-off planner overrides; the stored bucket values are left untouched.",
		PersistentPreRunE: preflight.PreRunE,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return preflight.EnsureAgentSelections(cmd.Context(), preflight.AgentCheckOptions{
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: agents,
			})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			var interactive *bool
			if cmd.Flags().Changed("interactive") || envSet("TASKS_REPLAN_INTERACTIVE") {
				v := viper.GetBool("tasks.replan.interactive")
				interactive = &v
			}
			return RunRePlan(cmd.Context(), RePlanOptions{
				FromTask:    viper.GetString("tasks.replan.from_task"),
				Tool:        viper.GetString("tasks.replan.tool"),
				Model:       viper.GetString("tasks.replan.model"),
				Interactive: interactive,
				Stdin:       cmd.InOrStdin(),
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
				Agents:      agents,
			})
		},
	}
	cmd.Flags().String("from-task", "", "Existing task id to re-plan (empty triggers the picker)")
	cmd.Flags().String("tool", "", "Planner tool override (cursor|claude); does not update the bucket")
	cmd.Flags().String("model", "", "Planner model override; does not update the bucket")
	cmd.Flags().Bool("interactive", false, "Run planner in interactive (TUI) mode")
	_ = viper.BindPFlag("tasks.replan.from_task", cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("tasks.replan.from_task", "TASKS_REPLAN_FROM_TASK")
	_ = viper.BindPFlag("tasks.replan.tool", cmd.Flags().Lookup("tool"))
	_ = viper.BindEnv("tasks.replan.tool", "TASKS_REPLAN_TOOL")
	_ = viper.BindPFlag("tasks.replan.model", cmd.Flags().Lookup("model"))
	_ = viper.BindEnv("tasks.replan.model", "TASKS_REPLAN_MODEL")
	_ = viper.BindPFlag("tasks.replan.interactive", cmd.Flags().Lookup("interactive"))
	_ = viper.BindEnv("tasks.replan.interactive", "TASKS_REPLAN_INTERACTIVE")
	return cmd
}
