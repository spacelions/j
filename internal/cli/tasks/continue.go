package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/plan"
	"github.com/spacelions/j/internal/cli/preflight"
	"github.com/spacelions/j/internal/cli/verify"
	"github.com/spacelions/j/internal/cli/work"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/store/tasks"
)

// ContinueOptions configures RunContinue. Stdin/Stdout/Stderr default
// to the process streams; UI defaults to the same huh-backed task
// picker used by `j tasks discard` / `j tasks enter`; Selector defaults
// to a huh-backed agent selector. Agents must be supplied by the
// caller (the cobra wiring injects the cursor + claude pair, tests
// inject scripted ones).
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
	// Selector drives the agent-pick prompt(s) when
	// EnsureAgentSelections finds an empty bucket. Mirrors the
	// surface of plan/work UIs but stays minimal since the
	// markdown / source pickers are not relevant on continue.
	Selector AgentSelector

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
//  3. Validate agent selections via EnsureAgentSelections so any
//     missing bucket prompts once before the dispatch fires.
//  4. Dispatch by Task.Status onto the matching phase Run /
//     RunResume. Already-finished tasks (verify-done / completed)
//     short-circuit with `J: task <id> already finished`.
func RunContinue(ctx context.Context, opts ContinueOptions) (err error) {
	defer func() {
		if errors.Is(err, huh.ErrUserAborted) {
			err = nil
		}
	}()
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

	if err := EnsureAgentSelections(ctx, AgentCheckOptions{
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
		Agents: opts.Agents,
		UI:     opts.Selector,
	}); err != nil {
		return err
	}

	return dispatchByStatus(ctx, opts, task)
}

// resolveContinueTask centralises the --from-task vs picker decision.
// On --from-task it loads the named row directly; an unknown id
// surfaces the same `J: no task` message `j tasks enter` prints. On
// the empty path it opens the store, runs pickFromStore, and closes
// before returning so the file lock is released ahead of the agent
// invocation downstream.
func resolveContinueTask(ctx context.Context, opts ContinueOptions) (tasks.Task, bool, error) {
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		return tasks.Task{}, false, err
	}
	if opts.TaskID == "" {
		if _, statErr := os.Stat(tasksDir); errors.Is(statErr, fs.ErrNotExist) {
			banner.Fprintln(opts.Stdout, emptyMessage)
			return tasks.Task{}, false, nil
		}
	}
	s := tasks.Open(tasksDir)
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
				banner.Fprintln(opts.Stdout, noTaskMessage)
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
//	planning     -> plan.RunResume
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
		return plan.RunResume(ctx, plan.ResumeOptions{
			TaskID: t.ID,
			Stdin:  opts.Stdin,
			Stdout: opts.Stdout,
			Stderr: opts.Stderr,
			Agents: opts.Agents,
		})
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
		banner.Fprintf(opts.Stdout, "J: task %s already finished\n", t.ID)
		return nil
	case tasks.StatusHelp:
		return dispatchHelp(ctx, opts, t)
	}
	return fmt.Errorf("J: task %s has unsupported status %q", t.ID, t.Status)
}

// resumeFromPlanDone forks a detached `j tasks orchestrate
// --skip-planning=true --plan-requires-approval=false` child for the
// supplied taskID so the implicit-approval handoff drives worker →
// verifier without re-running the planner. Records the spawned PID
// + agent.log path on the row, prints the standard `J: task <id>
// resumed; tail -f <log>` line, and returns immediately.
func resumeFromPlanDone(ctx context.Context, opts ContinueOptions, taskID string) error {
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		return fmt.Errorf("J: ensure task dir: %w", err)
	}
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)
	pid, err := spawnDetachedOrchestrator(ctx, opts.JBinary, agentLogPath, []string{
		"tasks", "orchestrate",
		"--id", taskID,
		"--plan-requires-approval=false",
		"--skip-planning=true",
	})
	if err != nil {
		return err
	}
	stampSpawnOnRow(opts.Stderr, taskID, agentLogPath, pid)
	banner.RunningInBackground(opts.Stdout, fmt.Sprintf("task %s", taskID), pid, agentLogPath)
	return nil
}

// stampSpawnOnRow records BackgroundPID + AgentLogPath on the
// existing task row after a detached orchestrator spawn. Best-effort
// — any read / write error surfaces as a single warning on stderr.
// The detached child is already running, so we never roll back.
func stampSpawnOnRow(stderr io.Writer, taskID, agentLogPath string, pid int) {
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		banner.DangerousBox(stderr, "J: tasks dir: %v", err)
		return
	}
	s := tasks.Open(tasksDir)
	defer func() { _ = s.Close() }()
	row, err := s.GetTask(taskID)
	if err != nil {
		banner.DangerousBox(stderr, "J: tasks get %q: %v", taskID, err)
		return
	}
	row.AgentLogPath = agentLogPath
	row.BackgroundPID = pid
	if err := s.PutTask(row); err != nil {
		banner.DangerousBox(stderr, "J: tasks put: %v", err)
	}
}

// dispatchHelp picks a resume target for a `help` task. The latest
// completed phase wins — verify > work > plan when a phase end
// timestamp is present — because that is the phase that produced the
// failure mode the user is recovering from. When no phase timestamps
// are set we fall back to the resume cursor that is non-empty in the
// same precedence so a plan-time crash that never wrote PlanEndAt is
// still resumable. With no usable signal the dispatch errors instead
// of silently skipping.
//
// `help` rows inherit the always-interactive + (for plan) must-read
// + save-suffix contract from {plan,work,verify}.RunResume: those
// helpers force Interactive=true on resume regardless of the bucket
// value, so a help row whose first run went headless still lands in
// the TUI here where the user can answer the clarification turn.
func dispatchHelp(ctx context.Context, opts ContinueOptions, t tasks.Task) error {
	switch latestPhase(t) {
	case "verify":
		return verify.RunResume(ctx, verify.ResumeOptions{
			TaskID: t.ID,
			Stdin:  opts.Stdin,
			Stdout: opts.Stdout,
			Stderr: opts.Stderr,
			Agents: opts.Agents,
		})
	case "work":
		return work.RunResume(ctx, work.ResumeOptions{
			TaskID: t.ID,
			Stdin:  opts.Stdin,
			Stdout: opts.Stdout,
			Stderr: opts.Stderr,
			Agents: opts.Agents,
		})
	case "plan":
		return plan.RunResume(ctx, plan.ResumeOptions{
			TaskID: t.ID,
			Stdin:  opts.Stdin,
			Stdout: opts.Stdout,
			Stderr: opts.Stderr,
			Agents: opts.Agents,
		})
	}
	return fmt.Errorf("J: task %s in `help` has no resumable phase signal", t.ID)
}

// latestPhase returns "verify", "work", "plan", or "" depending on
// which phase has the freshest end timestamp (or, if none, which
// resume cursor is non-empty). Pulled out of dispatchHelp so the
// precedence is unit-testable in isolation.
func latestPhase(t tasks.Task) string {
	if v := latestEndAt(t); v != "" {
		return v
	}
	switch {
	case t.VerifyResumeCursor != "":
		return "verify"
	case t.WorkResumeCursor != "":
		return "work"
	case t.PlanResumeCursor != "":
		return "plan"
	}
	return ""
}

// latestEndAt picks the phase whose *EndAt timestamp is the most
// recent. Returns "" when every *EndAt is nil.
func latestEndAt(t tasks.Task) string {
	pairs := []struct {
		name string
		t    *time.Time
	}{
		{"verify", t.VerifyEndAt},
		{"work", t.WorkEndAt},
		{"plan", t.PlanEndAt},
	}
	var best string
	var bestT time.Time
	for _, p := range pairs {
		if p.t == nil {
			continue
		}
		if best == "" || p.t.After(bestT) {
			best = p.name
			bestT = *p.t
		}
	}
	return best
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
	if o.Selector == nil {
		o.Selector = picker.New(o.Stdin, o.Stderr)
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
	cmd := &cobra.Command{
		Use:   "continue",
		Short: "Continue a task by dispatching to the right phase based on status",
		Long: "Resolves a task (via --from-task or the shared picker) and dispatches " +
			"to the right phase based on its status: planning -> `j plan resume`, " +
			"plan-done -> `j work`, working -> `j work resume`, work-done -> " +
			"`j verify`, verifying -> `j verify resume`. Already-finished tasks " +
			"(verify-done, completed) print `J: task <id> already finished` and " +
			"exit 0; a `help` row resumes whichever phase produced the failure " +
			"(latest *EndAt wins, falling back to the non-empty resume cursor). " +
			"Validates that every agent bucket (planner, worker, verifier) has " +
			"a tool/model selection — prompting once per missing bucket — before " +
			"the dispatch fires.",
		PersistentPreRunE: preflight.PreRunE,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunContinue(cmd.Context(), ContinueOptions{
				TaskID: viper.GetString("tasks.continue.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().String("from-task", "", "Continue the named task without showing the picker")
	_ = viper.BindPFlag("tasks.continue.from_task", cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("tasks.continue.from_task", "TASKS_CONTINUE_FROM_TASK")
	return cmd
}
