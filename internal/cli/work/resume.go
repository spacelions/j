package work

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/cli/picker"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
)

// ResumeOptions configures RunResume. Stdin/Stdout/Stderr default to
// the process streams; UI defaults to the huh implementation; Agents
// must be supplied by the caller (the cobra wiring injects
// `[]codingagents.Agent{cursor.New(), claude.New()}`, tests inject scripted ones).
//
// TaskID short-circuits the selector: when non-empty Run loads that
// task directly. Otherwise Run lists every task whose
// WorkResumeCursor is non-empty (any status — explicitly NOT the
// validateForWork allowlist), prints
// "J: there are no resumable sessions" if the slice is empty,
// auto-selects the single eligible row when there is exactly one,
// and asks UI.PickWorkTask when there are multiple. The picker is
// labelled `Select a task to resume` by the huh UI.
type ResumeOptions struct {
	// TaskID is the optional `--from-task <id>` selector. When set
	// it skips the picker entirely and resumes the named task.
	TaskID string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI
}

func (o ResumeOptions) withDefaults() ResumeOptions {
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
		o.UI = picker.New(o.Stdin, o.Stderr)
	}
	return o
}

// RunResume implements `j work resume`. The lifecycle mirrors
// `j work --from-task` reuse with three key differences: the
// task's existing WorkResumeCursor is reused (no fresh
// NewResumeID), the original WorkBeginAt is preserved, and the
// eligibility filter is permissive — any task with a non-empty
// WorkResumeCursor qualifies, regardless of status. validateForWork
// is intentionally NOT called.
//
// Resume always runs interactive — by definition resume is iterative
// (the user drives the next step from the TUI), and headless mode
// has no stdin path back to the human. The worker bucket's
// `interactive` value is intentionally ignored on resume.
//
// A user-abort in the resume picker (huh.ErrUserAborted) is
// translated to a nil return by the deferred guard below so cancel
// exits cleanly without surfacing a "cancelled by user" line.
func RunResume(ctx context.Context, opts ResumeOptions) (err error) {
	defer func() {
		if errors.Is(err, huh.ErrUserAborted) {
			err = nil
		}
	}()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("J: no coding agents configured")
	}

	t, ok, err := resolveResumeTask(ctx, opts)
	if err != nil {
		return err
	}
	if !ok {
		banner.Fprintln(opts.Stdout, "J: there are no resumable sessions")
		return nil
	}

	agent, ok := lookupResumeAgent(opts.Agents, t.InvokedTool)
	if !ok {
		return fmt.Errorf("J: unknown tool %q", t.InvokedTool)
	}

	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		return err
	}
	taskDir := filepath.Join(tasksDir, t.ID)
	planPath := filepath.Join(taskDir, tasks.PlanFileName)
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)

	lc := t.BeginWorkResume(opts.Stderr, agentLogPath)
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		banner.DangerousBox(opts.Stderr, "J: %v", mustReadErr)
	}
	// Resume always runs interactive — clarification / iteration
	// answers need a TUI, and the worker bucket's `interactive`
	// value is intentionally ignored on resume. PID is always 0
	// since resume never goes headless.
	_, workErr := agent.Work(ctx, codingagents.WorkRequest{
		PlanPath:     planPath,
		Model:        t.InvokedModel,
		Interactive:  true,
		ResumeChatID: t.WorkResumeCursor,
		Resume:       true,
		MustRead:     mustReadFiles,
	})
	lc.Finish(workErr)
	if workErr != nil {
		return workErr
	}

	banner.Fprintf(opts.Stdout, "J: work resume on task %s\n", t.ID)
	return nil
}

// resolveResumeTask runs the --from-task / picker / single-task
// short-circuit flow and returns the chosen task. The bool result
// is false (with nil error) when no eligible tasks exist; callers
// should print the "no resumable sessions" line and return nil.
//
// The store is opened, queried, and closed inside this helper so
// the agent invocation in the caller does not hold the bbolt lock.
func resolveResumeTask(ctx context.Context, opts ResumeOptions) (tasks.Task, bool, error) {
	if opts.TaskID != "" {
		return resolveResumeByID(opts.TaskID)
	}
	rows, err := listResumableTasks()
	if err != nil {
		return tasks.Task{}, false, err
	}
	switch len(rows) {
	case 0:
		return tasks.Task{}, false, nil
	case 1:
		return rows[0], true, nil
	}
	chosen, ok, err := opts.UI.PickTask(ctx, "Select a task to resume", rows)
	if err != nil {
		return tasks.Task{}, false, err
	}
	if !ok {
		return tasks.Task{}, false, nil
	}
	for _, t := range rows {
		if t.ID == chosen {
			return t, true, nil
		}
	}
	return tasks.Task{}, false, fmt.Errorf("J: task %q not found", chosen)
}

// resolveResumeByID loads the named task and validates it has a
// non-empty WorkResumeCursor. fs.ErrNotExist becomes the friendly
// "task %q not found" wrapping the way callers expect; an empty
// cursor becomes "task %q has no work session".
func resolveResumeByID(id string) (tasks.Task, bool, error) {
	t, err := resolver.TaskByID("work", id)
	if err != nil {
		return tasks.Task{}, false, err
	}
	if t.WorkResumeCursor == "" {
		return tasks.Task{}, false, fmt.Errorf("J: task %q has no work session", id)
	}
	return t, true, nil
}

// listResumableTasks returns every task with a non-empty
// WorkResumeCursor regardless of status, sorted via tasks.SortTasks
// so the picker shows the active-then-most-recent order users see
// in `j tasks`. validateForWork is intentionally NOT applied here:
// resume is permissive by design, so `working` / `work-done` rows
// are also resumable.
func listResumableTasks() ([]tasks.Task, error) {
	all, err := resolver.ListAllTasks()
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, t := range all {
		if t.WorkResumeCursor != "" {
			out = append(out, t)
		}
	}
	return out, nil
}

// lookupResumeAgent returns the first agent in agents whose Name
// matches tool. The miss path becomes the user-facing
// `unknown tool %q` error in RunResume.
func lookupResumeAgent(agents []codingagents.Agent, tool string) (codingagents.Agent, bool) {
	for _, a := range agents {
		if a.Name() == tool {
			return a, true
		}
	}
	return nil, false
}

// newResumeCmd builds the `j work resume` cobra subcommand. It
// owns its own --from-task flag and the matching viper / env
// bindings so the parent `j work` Run is unchanged.
//
// viper.BindPFlag and viper.BindEnv only fail when their input is
// nil or empty — programmer errors that this function does not
// produce — so their returned errors are intentionally discarded
// (mirroring the parent New).
func newResumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume a previously started work session",
		Long: "Lists tasks whose work session is non-empty and resumes the chosen one " +
			"using the tool/model recorded on the task row. " +
			"Pass --from-task <id> (or WORK_RESUME_FROM_TASK) to skip the picker. " +
			"With no eligible sessions, prints `J: there are no resumable sessions` " +
			"and exits 0. The eligibility filter is intentionally permissive: tasks " +
			"in any status (including `working` and `work-done`) are resumable as " +
			"long as their work_resume_cursor is non-empty. " +
			"Resume always runs interactive; the worker bucket's `interactive` " +
			"value is ignored.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunResume(cmd.Context(), ResumeOptions{
				TaskID: viper.GetString("work.resume.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().String("from-task", "", "Resume the named task without showing the picker")
	_ = viper.BindPFlag("work.resume.from_task", cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("work.resume.from_task", "WORK_RESUME_FROM_TASK")
	return cmd
}
