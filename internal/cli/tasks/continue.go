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
	"github.com/spacelions/j/internal/cli/verify"
	"github.com/spacelions/j/internal/cli/work"
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

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	// UI drives the task picker. The same UI shape as `j tasks
	// enter` so the on-disk widget is shared.
	UI UI

	// JBinary is the absolute path to the j binary re-executed by
	// the plan-done branch as `j tasks orchestrate --skip-planning ...`.
	// Empty falls back to os.Executable. Tests inject a path-resolvable
	// stub.
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
//     RunResume. Already-finished tasks (verify-done / completed)
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
// Status. The mapping is:
//
//	planning     -> detached re-plan orchestrator
//	plan-done    -> detached `j tasks orchestrate --skip-planning ...`
//	working      -> work.RunResume
//	work-done    -> verify.Run (--from-task <id>)
//	verifying    -> verify.RunResume
//	verify-done  -> "task already finished" (no-op)
//	completed    -> "task already finished" (no-op)
//	help         -> dispatchHelp (timestamps + cursors)
//
// Any unknown status surfaces a user-facing error so a future state
// addition cannot silently drop into a no-op.
func dispatchByStatus(ctx context.Context, opts ContinueOptions, t tasks.Task) error {
	switch t.Status {
	case tasks.StatusPlanning:
		return replanAsDetachedOrchestrator(ctx, opts, t)
	case tasks.StatusPlanDone:
		return resumeFromPlanDone(ctx, opts, t.ID)
	case tasks.StatusWorking:
		return work.RunResume(ctx, work.ResumeOptions{
			TaskID: t.ID,
			Stdin:  opts.Stdin,
			Stdout: opts.Stdout,
			Stderr: opts.Stderr,
			Agents: opts.Agents,
		})
	case tasks.StatusWorkDone:
		return verify.Run(ctx, verify.Options{
			TaskID: t.ID,
			Stdin:  opts.Stdin,
			Stdout: opts.Stdout,
			Stderr: opts.Stderr,
			Agents: opts.Agents,
		})
	case tasks.StatusVerifying:
		return verify.RunResume(ctx, verify.ResumeOptions{
			TaskID: t.ID,
			Stdin:  opts.Stdin,
			Stdout: opts.Stdout,
			Stderr: opts.Stderr,
			Agents: opts.Agents,
		})
	case tasks.StatusVerifyDone, tasks.StatusCompleted:
		uitheme.NormalFprintf(opts.Stdout, "J: task %s already finished\n", t.ID)
		return nil
	case tasks.StatusHelp:
		return dispatchHelp(ctx, opts, t)
	}
	return fmt.Errorf("J: task %s has unsupported status %q", t.ID, t.Status)
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

// newContinueCmd builds the `j tasks continue` cobra subcommand. The
// flag surface mirrors the resume commands (--from-task) plus an
// --interactive pass-through that the dispatched phase honours
// (`j work --interactive=...` is forwarded into work.Options;
// resume phases ignore the flag because they read the bucket
// directly). viper.BindPFlag / viper.BindEnv only fail on programmer
// errors so their returned errors are intentionally discarded.
func newContinueCmd() *cobra.Command {
	agents := []codingagents.Agent{cursor.New(), claude.New()}
	cmd := &cobra.Command{
		Use:   "continue",
		Short: "Continue a task by dispatching to the right phase based on status",
		Long: "Resolves a task (via --from-task or the shared picker) and dispatches " +
			"to the right phase based on its status: planning -> detached re-plan, " +
			"plan-done -> `j tasks orchestrate --skip-planning`, working -> `j work resume`, work-done -> " +
			"`j verify`, verifying -> `j verify resume`. Already-finished tasks " +
			"(verify-done, completed) print `J: task <id> already finished` and " +
			"exit 0; a `help` row resumes whichever phase produced the failure " +
			"(latest *EndAt wins, falling back to the non-empty resume cursor). " +
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
			return RunContinue(cmd.Context(), ContinueOptions{
				TaskID: viper.GetString("tasks.continue.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: agents,
			})
		},
	}
	cmd.Flags().String("from-task", "", "Continue the named task without showing the picker")
	_ = viper.BindPFlag("tasks.continue.from_task", cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("tasks.continue.from_task", "TASKS_CONTINUE_FROM_TASK")
	return cmd
}
