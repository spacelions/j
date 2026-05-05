package plan

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

	"github.com/spacelions/j/internal/cli/uitheme"
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
// PlanResumeCursor is non-empty (any status), prints
// "J: there are no resumable sessions" if the slice is empty,
// auto-selects the single eligible row when there is exactly one,
// and asks UI.PickPlanTask when there are multiple.
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

func newResumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume a previously started plan session",
		Long: "Lists tasks whose plan session is non-empty and resumes the chosen one " +
			"using the tool/model recorded on the task row. Pass --from-task <id> " +
			"(or PLAN_RESUME_FROM_TASK) to skip the picker.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunResume(cmd.Context(), ResumeOptions{
				TaskID: viper.GetString("plan.resume.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().String("from-task", "", "Resume the named task without showing the picker")
	_ = viper.BindPFlag("plan.resume.from_task", cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("plan.resume.from_task", "PLAN_RESUME_FROM_TASK")
	return cmd
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

// RunResume implements `j plan resume`. The lifecycle mirrors
// `j plan`'s markdown path with two key differences: the task is
// reused in place (no new id, the original PlanBeginAt is
// preserved) and the agent / model are read from the task row
// instead of the picker / settings.
//
// Resume always runs interactive — clarification answers need a
// TUI, and the planner bucket's `interactive` value is intentionally
// ignored on resume (a help-status row whose first run went headless
// would otherwise re-spawn headless and the user still couldn't
// answer the clarification turn).
//
// The bbolt store is opened, written, and closed before agent.Plan
// runs, then re-opened to write the terminal status; the agent
// invocation never holds the file lock so a concurrent `j tasks`
// or `j plan resume` from another shell does not deadlock.
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
		uitheme.NormalFprintln(opts.Stdout, "J: there are no resumable sessions")
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
	requirementsPath := filepath.Join(taskDir, tasks.RequirementsFileName)
	planPath := filepath.Join(taskDir, tasks.PlanFileName)

	resumeTask := planResumeBegin(t)
	tasks.PersistWarn(opts.Stderr, resumeTask)

	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", mustReadErr)
	}

	// Resume always runs interactive — clarification answers need a
	// TUI, and the planner bucket's `interactive` value is
	// intentionally ignored on resume. The returned PID is always 0
	// because resume never goes headless via the background spawn
	// path.
	_, planErr := agent.Plan(ctx, codingagents.PlanRequest{
		FromFilePath:           requirementsPath,
		Model:                  t.InvokedModel,
		RequirementsOutputPath: requirementsPath,
		PlanOutputPath:         planPath,
		Interactive:            true,
		ResumeChatID:           t.PlanResumeCursor,
		Resume:                 true,
		MustRead:               mustReadFiles,
	})

	var refinedReq, planMD string
	if planErr == nil {
		refinedReq = readBestEffortWarn(opts.Stderr, requirementsPath)
		planMD = readBestEffortWarn(opts.Stderr, planPath)
	}
	tasks.PersistWarn(opts.Stderr, planResumeFinish(resumeTask, planErr, refinedReq, planMD, requirementsPath))
	if planErr != nil {
		return planErr
	}

	uitheme.NormalFprintf(opts.Stdout, "J: plan resume on task %s\n", t.ID)
	return nil
}

// resolveResumeTask runs the --from-task / picker / single-task
// short-circuit flow and returns the chosen task. The bool result
// is false (with nil error) when no eligible tasks exist; callers
// should print the "no resumable sessions" line and return nil.
//
// The store is opened, queried, and closed inside this helper so
// the agent invocation in the caller does not hold the bbolt lock
// (the same lock would otherwise block concurrent `j tasks` runs).
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
	chosen, ok, err := opts.UI.PickTask(ctx, "Select a plan session to resume", rows)
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
	return tasks.Task{}, false, fmt.Errorf("plan resume: task %q not found", chosen)
}

// resolveResumeByID loads the named task and validates it has a
// non-empty PlanResumeCursor. fs.ErrNotExist becomes the friendly
// "task %q not found" wrapping the way callers expect; an empty
// cursor becomes "task %q has no plan session".
func resolveResumeByID(id string) (tasks.Task, bool, error) {
	t, err := resolver.TaskByID(id)
	if err != nil {
		return tasks.Task{}, false, err
	}
	if t.PlanResumeCursor == "" {
		return tasks.Task{}, false, fmt.Errorf("J: task %q has no plan session", id)
	}
	return t, true, nil
}

// listResumableTasks returns every task with a non-empty
// PlanResumeCursor (any status), sorted via tasks.SortTasks so the
// picker shows the active-then-most-recent order users see in
// `j tasks`. The bbolt store is closed before the slice is
// returned so the agent invocation downstream does not contend on
// the file lock.
func listResumableTasks() ([]tasks.Task, error) {
	all, err := resolver.ListAllTasks()
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, t := range all {
		if t.PlanResumeCursor != "" {
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

// readBestEffortWarn reads path and returns the body. Errors yield
// an empty string and a stderr breadcrumb so users notice when the
// agent failed to write an expected output (e.g. requirements.md
// after a successful plan resume). Used post-run to feed the
// tasklog summary derivation.
