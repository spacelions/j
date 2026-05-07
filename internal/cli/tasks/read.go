package tasks

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/store/tasks"
)

// ReadOptions configures the `j tasks read requirements` and
// `j tasks read plan` leaves. Stdin/Stdout/Stderr default to the
// process streams; UI defaults to the huh-backed picker; Viewer
// defaults to defaultViewer (bat -> cat -> io.Copy). Tests pass
// scripted fakes for both UI and Viewer to avoid spawning real
// subprocesses, mirroring EnterOptions.
type ReadOptions struct {
	// TaskID is the optional --from-task selector. Empty triggers
	// the shared picker fallback.
	TaskID string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	UI     UI
	Viewer Viewer
}

func (o ReadOptions) withDefaults() ReadOptions {
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

// runReadFile is the shared body of the read leaves: resolve the
// per-task file via resolveTaskFile and hand the absolute path to
// the injected Viewer. resolveTaskFile prints the user-facing
// short-circuit messages itself, so the leaf only forwards the
// happy-path call.
func runReadFile(
	ctx context.Context,
	opts ReadOptions,
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

// RunReadRequirements is the entry point for
// `j tasks read requirements`.
func RunReadRequirements(ctx context.Context, opts ReadOptions) error {
	return runReadFile(ctx, opts, tasks.RequirementsFileName)
}

// RunReadPlan is the entry point for `j tasks read plan`.
func RunReadPlan(ctx context.Context, opts ReadOptions) error {
	return runReadFile(ctx, opts, tasks.PlanFileName)
}

// newReadCmd builds the parent `read` cobra group and attaches the
// requirements / plan leaves. The parent has no RunE so cobra prints
// the leaf list when invoked without a subcommand.
func newReadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read",
		Short: "Render a task's requirements.md or plan.md",
		Long: "Renders a per-task artefact (requirements.md or " +
			"plan.md) from <cwd>/.j/tasks/<id>/ via bat (when " +
			"installed and stdout is a TTY) or cat. Read-only: no " +
			"row or file is mutated.",
	}
	cmd.AddCommand(newReadRequirementsCmd())
	cmd.AddCommand(newReadPlanCmd())
	return cmd
}

func newReadRequirementsCmd() *cobra.Command {
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
			return RunReadRequirements(cmd.Context(), ReadOptions{
				TaskID: viper.GetString(
					"tasks.read.requirements.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().String("from-task", "",
		"Render the named task's requirements.md (no picker)")
	_ = viper.BindPFlag("tasks.read.requirements.from_task",
		cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("tasks.read.requirements.from_task",
		"TASKS_READ_REQUIREMENTS_FROM_TASK")
	return cmd
}

func newReadPlanCmd() *cobra.Command {
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
			return RunReadPlan(cmd.Context(), ReadOptions{
				TaskID: viper.GetString(
					"tasks.read.plan.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().String("from-task", "",
		"Render the named task's plan.md (no picker)")
	_ = viper.BindPFlag("tasks.read.plan.from_task",
		cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("tasks.read.plan.from_task",
		"TASKS_READ_PLAN_FROM_TASK")
	return cmd
}
