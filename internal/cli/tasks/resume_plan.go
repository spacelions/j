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

// noActivePlanSessionMessage is the single line printed to stdout
// when no task in the bbolt store has a non-empty PlanResumeSession.
// Pinning it in a constant keeps the test assertion and the command
// output in lockstep.
const noActivePlanSessionMessage = "J: no tasks with an active plan session"

// ResumePlanOptions configures RunResumePlan. Stdin/Stdout/Stderr
// default to the process streams; UI defaults to the huh-backed task
// picker; Agents must be supplied by the caller (the cobra wiring
// injects `[]codingagents.Agent{cursor.New(), claude.New()}`, tests
// inject scripted ones).
type ResumePlanOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI

	// Interactive, when true, runs the orchestrator in the foreground
	// so the planner's TUI inherits the user's terminal. When false,
	// the orchestrator is spawned as a detached background process.
	// Defaults to true when wired through the cobra command.
	Interactive bool

	// JBinary is the absolute path to the j binary re-executed as
	// `j tasks orchestrate --id <id>`. Empty falls back to
	// os.Executable. Tests inject a path-resolvable stub.
	JBinary string
}

func (o ResumePlanOptions) withDefaults() ResumePlanOptions {
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

// RunResumePlan implements `j tasks resume-plan`. It filters the
// bbolt store to rows whose PlanResumeSession is non-empty, prompts
// the user to pick one, and launches the orchestrator. When
// Interactive is true (the default through the cobra command), the
// orchestrator runs in the foreground so the planner's TUI inherits
// the user's terminal; when false, it spawns as a detached
// background process.
func RunResumePlan(ctx context.Context, opts ResumePlanOptions) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()

	taskID, ok, err := resolveResumePlanTaskID(ctx, opts)
	if err != nil || !ok {
		return err
	}
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		return fmt.Errorf("J: ensure task dir: %w", err)
	}
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)

	args := []string{
		"tasks", "orchestrate",
		"--id", taskID,
		"--plan-requires-approval=true",
	}
	if opts.Interactive {
		args = append(args, "--interactive="+strconv.FormatBool(opts.Interactive))
	}

	if opts.Interactive {
		return runInlineOrchestrator(ctx, opts.JBinary, args)
	}

	pid, err := spawnDetachedOrchestrator(ctx, opts.JBinary, agentLogPath, args)
	if err != nil {
		return err
	}
	stampSpawnOnRow(opts.Stderr, taskID, agentLogPath, pid)
	uitheme.NormalForkDialog(opts.Stdout, fmt.Sprintf("task %s", taskID), pid, agentLogPath)
	return nil
}

func resolveResumePlanTaskID(ctx context.Context, opts ResumePlanOptions) (string, bool, error) {
	s, err := tasks.OpenDefault()
	if err != nil {
		return "", false, err
	}
	id, ok, err := pickResumePlanFromStore(ctx, s, opts)
	_ = s.Close()
	return id, ok, err
}

func pickResumePlanFromStore(ctx context.Context, s *tasks.Store, opts ResumePlanOptions) (string, bool, error) {
	rows, err := s.ListTasks()
	if err != nil {
		return "", false, err
	}
	filtered := filterTasksWithPlanSession(rows)
	if len(filtered) == 0 {
		uitheme.NormalFprintln(opts.Stdout, noActivePlanSessionMessage)
		return "", false, nil
	}
	tasks.SortTasks(filtered)
	return opts.UI.PickTask(ctx, filtered)
}

func filterTasksWithPlanSession(rows []tasks.Task) []tasks.Task {
	out := make([]tasks.Task, 0, len(rows))
	for _, t := range rows {
		if t.PlanResumeSession != "" {
			out = append(out, t)
		}
	}
	return out
}

// newResumePlanCmd builds the `j tasks resume-plan` cobra subcommand.
// The picker filters to rows with a recorded plan resume session so
// only tasks the agent actually started can be resumed; an empty
// list short-circuits with a user-facing message and exit 0.
// --interactive defaults to true: the orchestrator runs in the
// foreground so the planner's TUI inherits the terminal. Pass
// --interactive=false to use the detached background mode instead.
func newResumePlanCmd() *cobra.Command {
	agents := []codingagents.Agent{cursor.New(), claude.New()}
	cmd := &cobra.Command{
		Use:   "resume-plan",
		Short: "Resume an in-flight planner session",
		Long: "Filters tasks to rows with a non-empty plan_resume_session and " +
			"renders the shared task picker. The selected task launches the " +
			"orchestrator: in foreground by default (--interactive=true) so the " +
			"planner's TUI inherits the terminal, or detached (--interactive=false). " +
			"When no task carries an active plan session, prints `J: no tasks with " +
			"an active plan session` and exits 0.",
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
			return RunResumePlan(cmd.Context(), ResumePlanOptions{
				Stdin:       cmd.InOrStdin(),
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
				Agents:      agents,
				Interactive: viper.GetBool("tasks.resume-plan.interactive"),
			})
		},
	}
	cmd.Flags().Bool("interactive", true, "Run planner in interactive (TUI) mode")
	_ = viper.BindPFlag("tasks.resume-plan.interactive", cmd.Flags().Lookup("interactive"))
	_ = viper.BindEnv("tasks.resume-plan.interactive", "TASKS_RESUME_PLAN_INTERACTIVE")
	return cmd
}
