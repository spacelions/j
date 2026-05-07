package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/preflight"
	"github.com/spacelions/j/internal/cli/uitheme"
	"path/filepath"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
)

// ContinueOptions configures RunContinue. Stdin/Stdout/Stderr default
// to the process streams; UI defaults to the same huh-backed task
// picker used by `j tasks discard` / `j tasks enter`. Agents must be
// supplied by the caller (the cobra wiring injects the cursor + claude
// pair, tests inject scripted ones).
type ContinueOptions struct {
	// TaskID is the optional `--from-task <id>` selector. When set
	// it skips the picker entirely and dispatches directly. An
	// empty value triggers the existing pickFromStore widget over
	// every task in the bbolt store.
	TaskID string

	// Tool and Model are the --tool / --model overrides for plan-done
	// dispatch. When set they are forwarded to worker.Run as explicit
	// values; when empty RunRead reads from the stored worker bucket.
	Tool  string
	Model string

	// Interactive is the resolved --interactive flag for plan-done
	// dispatch. When the cobra flag is explicitly changed (or the env
	// var is set) it passes the resolved value; when unset
	// resolver.Interactive picks the default from the stored bucket.
	Interactive *bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	// UI drives the task picker. The same UI shape as `j tasks
	// enter` so the on-disk widget is shared.
	UI UI

	// JBinary is the absolute path to the j binary re-executed by
	// the planning-status dispatch paths. Empty falls back to
	// os.Executable. Tests inject a path-resolvable stub.
	JBinary string
}

// RunContinue implements `j tasks continue`. The lifecycle is:
//
//  1. Defer a huh.ErrUserAborted -> nil guard so a Ctrl-C in any
//     downstream prompt exits cleanly.
//  2. Resolve the target task: --from-task or pickFromStore (the
//     same picker `j tasks enter` uses). An empty store prints
//     the standard `J: no tasks` message and returns nil; a
//     user-cancel in the picker also returns nil.
//  3. Dispatch by Task.Status onto the matching phase Run /
//     RunResume. Already-finished tasks (failed / completed)
//     short-circuit with `J: task <id> already finished`.
func RunContinue(ctx context.Context, opts ContinueOptions) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("J: no coding agents configured")
	}

	task, ok, err := resolveContinueTask(ctx, opts)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	return dispatchByStatus(ctx, opts, task)
}

// resolveContinueTask centralises the --from-task vs picker decision.
// On --from-task it loads the named row directly; an unknown id
// surfaces the same `J: no task` message `j tasks enter` prints. On
// the empty path pickFromStore prints emptyMessage when the store is
// empty (or the dir is missing) — ListTasks treats both as an empty
// list, so this site doesn't need its own short-circuit.
func resolveContinueTask(ctx context.Context, opts ContinueOptions) (tasks.Task, bool, error) {
	s, err := tasks.OpenDefault()
	if err != nil {
		return tasks.Task{}, false, err
	}
	task, ok, err := resolveContinueTaskFromStore(ctx, s, opts)
	_ = s.Close()
	return task, ok, err
}

// resolveContinueTaskFromStore is the inner half of resolveContinueTask:
// once a store handle is open it either loads the named id or runs the
// shared picker. Splitting it out keeps the open/close cycle in one
// place so the lock release is structurally guaranteed.
func resolveContinueTaskFromStore(ctx context.Context, s *tasks.Store, opts ContinueOptions) (tasks.Task, bool, error) {
	if opts.TaskID != "" {
		t, err := s.GetTask(opts.TaskID)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				uitheme.NormalFprintln(opts.Stdout, noTaskMessage)
				return tasks.Task{}, false, nil
			}
			return tasks.Task{}, false, err
		}
		return t, true, nil
	}
	id, ok, err := pickFromStore(ctx, s, opts.UI, opts.Stdout)
	if err != nil || !ok {
		return tasks.Task{}, false, err
	}
	t, err := s.GetTask(id)
	if err != nil {
		return tasks.Task{}, false, err
	}
	return t, true, nil
}

// dispatchByStatus routes a task to the right phase based on its
// Status. For planning and working statuses it prints a tooltip
// directing the user to use the specific subcommand.
func dispatchByStatus(ctx context.Context, opts ContinueOptions, t tasks.Task) error {
	switch t.Status {
	case tasks.StatusPlanning:
		uitheme.NormalFprintf(opts.Stdout, "J: task %s is planning; use `j tasks re-plan` or `j tasks resume-plan`\n", t.ID)
		return nil
	case tasks.StatusPlanDone:
		return runPlanDoneWork(ctx, opts, t)
	case tasks.StatusWorking:
		uitheme.NormalFprintf(opts.Stdout, "J: task %s is working; use `j tasks re-work` or `j tasks resume-work`\n", t.ID)
		return nil
	case tasks.StatusWorkDone:
		return reverifyAsDetachedOrchestrator(ctx, opts, t.ID)
	case tasks.StatusVerifying:
		return resumeVerifyingInline(ctx, opts, t.ID)
	case tasks.StatusFailed, tasks.StatusCompleted:
		uitheme.NormalFprintf(opts.Stdout, "J: task %s already finished\n", t.ID)
		return nil
	case tasks.StatusHelp:
		return dispatchHelp(ctx, opts, t)
	}
	return fmt.Errorf("J: task %s has unsupported status %q", t.ID, t.Status)
}

func reverifyAsDetachedOrchestrator(ctx context.Context, opts ContinueOptions, taskID string) error {
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		return fmt.Errorf("J: ensure task dir: %w", err)
	}
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)
	pid, err := spawnDetachedOrchestrator(ctx, opts.JBinary, agentLogPath, []string{
		"tasks", "orchestrate",
		"--id", taskID,
		"--phase=verify-only",
	})
	if err != nil {
		return err
	}
	stampSpawnOnRow(opts.Stderr, taskID, agentLogPath, pid)
	uitheme.NormalForkDialog(opts.Stdout, fmt.Sprintf("task %s", taskID), pid, agentLogPath)
	return nil
}

func resumeVerifyingInline(ctx context.Context, opts ContinueOptions, taskID string) error {
	if _, err := tasks.EnsureDir(taskID); err != nil {
		return err
	}
	return runInlineOrchestrator(ctx, opts.JBinary, []string{
		"tasks", "orchestrate",
		"--id", taskID,
		"--phase=verify-only",
		"--interactive=true",
	})
}

func (o ContinueOptions) withDefaults() ContinueOptions {
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

// newContinueCmd builds the `j tasks continue` cobra subcommand with
// --from-task, --tool, --model, and --interactive flags. The --tool,
// --model, and --interactive flags are forwarded into worker.Run on the
// plan-done dispatch path; resume phases ignore them. viper.BindPFlag
// / viper.BindEnv only fail on programmer errors so their errors are
// intentionally discarded.
func newContinueCmd() *cobra.Command {
	agents := []codingagents.Agent{cursor.New(), claude.New()}
	cmd := &cobra.Command{
		Use:   "continue",
		Short: "Continue a task by dispatching to the right phase based on status",
		Long: "Resolves a task (via --from-task or the shared picker) and dispatches " +
			"to the right phase based on its status: planning -> detached re-plan, " +
			"plan-done -> direct worker run, working -> work resume, " +
			"work-done -> `j verify`, verifying -> `j verify resume`. " +
			"Already-finished tasks (failed, completed) print " +
			"`J: task <id> already finished` and exit 0; a `help` row " +
			"resumes whichever phase produced the failure (latest *EndAt " +
			"wins, falling back to the non-empty resume cursor). " +
			"Validates that every agent bucket (planner, worker, verifier) has " +
			"a tool/model selection — prompting once per missing bucket — before " +
			"the dispatch fires.",
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
			if cmd.Flags().Changed("interactive") || envSet("TASKS_CONTINUE_INTERACTIVE") {
				v := viper.GetBool("tasks.continue.interactive")
				interactive = &v
			}
			return RunContinue(cmd.Context(), ContinueOptions{
				TaskID:      viper.GetString("tasks.continue.from_task"),
				Tool:        viper.GetString("tasks.continue.tool"),
				Model:       viper.GetString("tasks.continue.model"),
				Interactive: interactive,
				Stdin:       cmd.InOrStdin(),
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
				Agents:      agents,
			})
		},
	}
	cmd.Flags().String("from-task", "", "Continue the named task without showing the picker")
	cmd.Flags().String("tool", "", "Coding agent tool for plan-done dispatch (cursor|claude)")
	cmd.Flags().String("model", "", "Model identifier for plan-done dispatch")
	cmd.Flags().Bool("interactive", true, "Launch the coding agent in interactive mode on plan-done dispatch")
	_ = viper.BindPFlag("tasks.continue.from_task", cmd.Flags().Lookup("from-task"))
	_ = viper.BindPFlag("tasks.continue.tool", cmd.Flags().Lookup("tool"))
	_ = viper.BindPFlag("tasks.continue.model", cmd.Flags().Lookup("model"))
	_ = viper.BindPFlag("tasks.continue.interactive", cmd.Flags().Lookup("interactive"))
	_ = viper.BindEnv("tasks.continue.from_task", "TASKS_CONTINUE_FROM_TASK")
	_ = viper.BindEnv("tasks.continue.tool", "TASKS_CONTINUE_TOOL")
	_ = viper.BindEnv("tasks.continue.model", "TASKS_CONTINUE_MODEL")
	_ = viper.BindEnv("tasks.continue.interactive", "TASKS_CONTINUE_INTERACTIVE")
	return cmd
}
