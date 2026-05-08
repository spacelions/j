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

// ReWorkOptions configures RunReWork.
type ReWorkOptions struct {
	FromTask string
	Tool     string
	Model    string
	// Interactive, when non-nil, overrides the worker's interactive
	// flag. nil means inherit the stored bucket value.
	Interactive *bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     RePlanUI

	JBinary string
}

func (o ReWorkOptions) withDefaults() ReWorkOptions {
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

// RunReWork implements `j tasks re-work`. It resolves a task, confirms
// status override, and re-execs `j tasks orchestrate --phase=from-work`.
func RunReWork(ctx context.Context, opts ReWorkOptions) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()

	taskID, ok, err := resolveRePlanTaskID(ctx, RePlanOptions{
		FromTask: opts.FromTask,
		UI:       opts.UI,
		Stdin:    opts.Stdin,
		Stdout:   opts.Stdout,
		Stderr:   opts.Stderr,
	})
	if err != nil || !ok {
		return err
	}
	task, err := resolver.TaskByID(taskID)
	if err != nil {
		return err
	}
	if !tasks.IsLegal(task.Status, tasks.EventWorkRestart) {
		return fmt.Errorf("cannot re-work task in status %q", task.Status)
	}
	proceed, err := resolver.ConfirmStatusOverride(
		ctx, opts.UI, false, "re-work", task, resolver.ReplanAllowed)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	taskDir, err := tasks.EnsureDir(task.ID)
	if err != nil {
		return fmt.Errorf("ensure task dir: %w", err)
	}
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)

	// Re-work means "start the worker fresh"; clearing
	// WorkResumeSession before re-execing the orchestrator is how
	// worker.Execute distinguishes re-work from resume-work (the
	// former mints a new session via NewResumeID, the latter sees
	// the populated row and feeds the existing id into `--resume`).
	if err := clearWorkResumeSession(task.ID); err != nil {
		return err
	}

	interactive := resolver.Interactive(opts.Interactive)

	args := []string{
		"tasks", "orchestrate",
		"--id", task.ID,
		"--phase=from-work",
		"--interactive=" + strconv.FormatBool(interactive),
	}
	if opts.Tool != "" {
		args = append(args, "--tool="+opts.Tool)
	}
	if opts.Model != "" {
		args = append(args, "--model="+opts.Model)
	}

	if interactive {
		stampSpawnOnRow(opts.Stderr, task.ID, "", 0)
		return runInlineOrchestrator(ctx, opts.JBinary, args)
	}

	pid, err := spawnDetachedOrchestrator(
		ctx, opts.JBinary, agentLogPath, args)
	if err != nil {
		return err
	}
	stampSpawnOnRow(opts.Stderr, task.ID, agentLogPath, pid)
	uitheme.NormalForkDialog(
		opts.Stdout, fmt.Sprintf("task %s", task.ID), pid, agentLogPath)
	return nil
}

// clearWorkResumeSession blanks the task row's WorkResumeSession in
// place. The orchestrator's worker phase treats a populated session
// as the "resume" signal, so callers that want a fresh worker run
// (re-work) must drop the field before re-execing.
func clearWorkResumeSession(taskID string) error {
	s, err := tasks.OpenDefault()
	if err != nil {
		return fmt.Errorf("open task store: %w", err)
	}
	defer func() { _ = s.Close() }()
	row, err := s.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("read task %s: %w", taskID, err)
	}
	if row.WorkResumeSession == "" {
		return nil
	}
	row.WorkResumeSession = ""
	if err := s.PutTask(row); err != nil {
		return fmt.Errorf("clear work resume session: %w", err)
	}
	return nil
}

// newReWorkCmd builds the `j tasks re-work` cobra subcommand.
func newReWorkCmd() *cobra.Command {
	agents := []codingagents.Agent{cursor.New(), claude.New()}
	cmd := &cobra.Command{
		Use: "re-work",
		Short: "Re-work an existing task: run the worker inline " +
			"(--interactive) or detached",
		Long: "Resolves a task (via --from-task or the shared picker) and " +
			"either re-execs `j tasks orchestrate --phase=from-work` " +
			"inline (with --interactive=true so the TUI can render in the " +
			"parent's terminal) or forks it as a detached child so the " +
			"worker re-runs without the user waiting in-process. Tasks in " +
			"plan-done or help skip the status-override prompt; any other " +
			"status renders a yes/no confirm before the orchestrator " +
			"runs. --tool / --model / --interactive forward into the " +
			"orchestrate argv as one-off worker overrides; the stored " +
			"bucket values are left untouched.",
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
			var interactive *bool
			if cmd.Flags().Changed("interactive") ||
				envSet("TASKS_REWORK_INTERACTIVE") {
				v := viper.GetBool("tasks.rework.interactive")
				interactive = &v
			}
			return RunReWork(cmd.Context(), ReWorkOptions{
				FromTask:    viper.GetString("tasks.rework.from_task"),
				Tool:        viper.GetString("tasks.rework.tool"),
				Model:       viper.GetString("tasks.rework.model"),
				Interactive: interactive,
				Stdin:       cmd.InOrStdin(),
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
				Agents:      agents,
			})
		},
	}
	cmd.Flags().String("from-task", "",
		"Existing task id to re-work (empty triggers the picker)")
	cmd.Flags().String("tool", "",
		"Worker tool override (cursor|claude); does not update the bucket")
	cmd.Flags().String("model", "",
		"Worker model override; does not update the bucket")
	cmd.Flags().Bool("interactive", false,
		"Run worker in interactive (TUI) mode")
	_ = viper.BindPFlag(
		"tasks.rework.from_task", cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("tasks.rework.from_task", "TASKS_REWORK_FROM_TASK")
	_ = viper.BindPFlag("tasks.rework.tool", cmd.Flags().Lookup("tool"))
	_ = viper.BindEnv("tasks.rework.tool", "TASKS_REWORK_TOOL")
	_ = viper.BindPFlag("tasks.rework.model", cmd.Flags().Lookup("model"))
	_ = viper.BindEnv("tasks.rework.model", "TASKS_REWORK_MODEL")
	_ = viper.BindPFlag(
		"tasks.rework.interactive", cmd.Flags().Lookup("interactive"))
	_ = viper.BindEnv("tasks.rework.interactive", "TASKS_REWORK_INTERACTIVE")
	return cmd
}
