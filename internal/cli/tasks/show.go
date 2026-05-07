package tasks

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/store/tasks"
)

// ShowOptions configures `j tasks show` and its leaves
// `j tasks show requirements` / `j tasks show plan`.
// Stdin/Stdout/Stderr default to the process streams; UI defaults to
// the huh-backed picker; Viewer defaults to defaultViewer (bat -> cat
// -> io.Copy). Tests pass scripted fakes for both UI and Viewer.
type ShowOptions struct {
	// TaskID is the optional --from-task selector. Empty triggers
	// the shared picker fallback.
	TaskID string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	UI     UI
	Viewer Viewer
}

func (o ShowOptions) withDefaults() ShowOptions {
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
	if o.Viewer == nil {
		o.Viewer = defaultViewer
	}
	return o
}

// runShowFile is the shared body of the show leaves: resolve the
// per-task file via resolveTaskFile and hand the absolute path to
// the injected Viewer.
func runShowFile(
	ctx context.Context,
	opts ShowOptions,
	filename string,
) error {
	opts = opts.withDefaults()
	path, ok, err := resolveTaskFile(ctx, fileResolveOptions{
		TaskID: opts.TaskID,
		UI:     opts.UI,
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	}, filename)
	if err != nil || !ok {
		return err
	}
	return opts.Viewer(
		ctx, path, opts.Stdin, opts.Stdout, opts.Stderr,
	)
}

// RunShow is the entry point for `j tasks show`. Renders the
// resolved task's task.toml.
func RunShow(ctx context.Context, opts ShowOptions) error {
	return runShowFile(ctx, opts, tasks.TaskFileName)
}

// RunShowRequirements is the entry point for
// `j tasks show requirements`.
func RunShowRequirements(ctx context.Context, opts ShowOptions) error {
	return runShowFile(ctx, opts, tasks.RequirementsFileName)
}

// RunShowPlan is the entry point for `j tasks show plan`.
func RunShowPlan(ctx context.Context, opts ShowOptions) error {
	return runShowFile(ctx, opts, tasks.PlanFileName)
}

// RunShowClarification is the entry point for
// `j tasks show clarification`.
func RunShowClarification(ctx context.Context, opts ShowOptions) error {
	return runShowFile(ctx, opts, "clarification.md")
}

// newShowCmd builds the parent `show` cobra command and attaches
// the requirements / plan leaves. The parent RunE renders the
// resolved task's task.toml; the leaves render requirements.md
// and plan.md respectively.
func newShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Render a task's task.toml, requirements.md, or plan.md",
		Long: "Resolves a task (via --from-task or the shared picker) " +
			"and renders the chosen artefact via bat (when installed " +
			"and stdout is a TTY) or cat. Without a subcommand the " +
			"task.toml is shown; `show requirements` and `show plan` " +
			"render the respective markdown files. Read-only.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunShow(cmd.Context(), ShowOptions{
				TaskID: viper.GetString(
					"tasks.show.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().String("from-task", "",
		"Render the named task's task.toml (no picker)")
	_ = viper.BindPFlag("tasks.show.from_task",
		cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("tasks.show.from_task",
		"TASKS_SHOW_FROM_TASK")
	cmd.AddCommand(newShowRequirementsCmd())
	cmd.AddCommand(newShowPlanCmd())
	cmd.AddCommand(newShowClarificationCmd())
	return cmd
}

func newShowRequirementsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "requirements",
		Short: "Render the resolved task's requirements.md",
		Long: "Renders <cwd>/.j/tasks/<id>/requirements.md via bat " +
			"(when installed and stdout is a TTY) or cat. Resolves " +
			"the task via --from-task or the shared picker; an " +
			"unknown id prints `J: no task` and a missing file " +
			"prints `J: requirements.md not found for task <id>`. " +
			"Both short-circuit exits 0 with no subprocess.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunShowRequirements(cmd.Context(), ShowOptions{
				TaskID: viper.GetString(
					"tasks.show.requirements.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().String("from-task", "",
		"Render the named task's requirements.md (no picker)")
	_ = viper.BindPFlag("tasks.show.requirements.from_task",
		cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("tasks.show.requirements.from_task",
		"TASKS_SHOW_REQUIREMENTS_FROM_TASK")
	return cmd
}

func newShowPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Render the resolved task's plan.md",
		Long: "Renders <cwd>/.j/tasks/<id>/plan.md via bat (when " +
			"installed and stdout is a TTY) or cat. Resolves the " +
			"task via --from-task or the shared picker; an unknown " +
			"id prints `J: no task` and a missing file prints " +
			"`J: plan.md not found for task <id>`. Both short-" +
			"circuit exits 0 with no subprocess.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunShowPlan(cmd.Context(), ShowOptions{
				TaskID: viper.GetString(
					"tasks.show.plan.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().String("from-task", "",
		"Render the named task's plan.md (no picker)")
	_ = viper.BindPFlag("tasks.show.plan.from_task",
		cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("tasks.show.plan.from_task",
		"TASKS_SHOW_PLAN_FROM_TASK")
	return cmd
}

func newShowClarificationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clarification",
		Short: "Render the task's clarification.md when present",
		Long: "Renders <cwd>/.j/tasks/<id>/clarification.md via bat " +
			"(when installed and stdout is a TTY) or cat. Resolves " +
			"the task via --from-task or the shared picker; an " +
			"unknown id prints `J: no task` and a missing file " +
			"prints `J: clarification.md not found for task <id>`. " +
			"Both short-circuit exits 0 with no subprocess.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunShowClarification(cmd.Context(), ShowOptions{
				TaskID: viper.GetString(
					"tasks.show.clarification.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().String("from-task", "",
		"Render the named task's clarification.md (no picker)")
	_ = viper.BindPFlag("tasks.show.clarification.from_task",
		cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("tasks.show.clarification.from_task",
		"TASKS_SHOW_CLARIFICATION_FROM_TASK")
	return cmd
}
