package plan

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

	"github.com/spacelions/j/internal/cli/agentpick"
	"github.com/spacelions/j/internal/cli/tasklog"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/store"
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
		o.UI = newHuhUI(o.Stdin, o.Stderr)
	}
	return o
}

// RunResume implements `j plan resume`. The lifecycle mirrors
// `j plan`'s markdown path with two key differences: the task is
// reused in place (no new id, the original PlanBeginAt is
// preserved) and the agent / model are read from the task row
// instead of the picker / settings. Resume is always interactive.
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
	interactive := resolveResumeInteractive(opts)

	task, ok, err := resolveResumeTask(ctx, opts)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(opts.Stdout, "J: there are no resumable sessions")
		return nil
	}

	agent, ok := lookupResumeAgent(opts.Agents, task.InvokedTool)
	if !ok {
		return fmt.Errorf("J: unknown tool %q", task.InvokedTool)
	}

	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		return err
	}
	taskDir := filepath.Join(tasksDir, task.ID)
	requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
	planPath := filepath.Join(taskDir, store.PlanFileName)

	resumeTask := planResumeBegin(task)
	tasklog.PersistWarn(opts.Stderr, resumeTask)

	// Resume Interactive precedence: explicit ResumeOptions.Interactive
	// > planner bucket's stored interactive > cobra default true. The
	// returned PID is always 0 because resume never goes headless via
	// the background spawn path.
	_, planErr := agent.Plan(ctx, codingagents.PlanRequest{
		FromFilePath:           requirementsPath,
		Model:                  task.InvokedModel,
		RequirementsOutputPath: requirementsPath,
		PlanOutputPath:         planPath,
		Interactive:            interactive,
		ResumeChatID:           task.PlanResumeCursor,
		Resume:                 true,
	})

	var refinedReq, planMD string
	if planErr == nil {
		refinedReq = readBestEffortWarn(opts.Stderr, requirementsPath)
		planMD = readBestEffortWarn(opts.Stderr, planPath)
	}
	tasklog.PersistWarn(opts.Stderr, planResumeFinish(resumeTask, planErr, refinedReq, planMD, requirementsPath))
	if planErr != nil {
		return planErr
	}

	fmt.Fprintf(opts.Stdout, "J: plan resume on task %s\n", task.ID)
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
func resolveResumeTask(ctx context.Context, opts ResumeOptions) (store.Task, bool, error) {
	if opts.TaskID != "" {
		return resolveResumeByID(opts.Stderr, opts.TaskID)
	}
	tasks, err := listResumableTasks(opts.Stderr)
	if err != nil {
		return store.Task{}, false, err
	}
	switch len(tasks) {
	case 0:
		return store.Task{}, false, nil
	case 1:
		return tasks[0], true, nil
	}
	chosen, ok, err := opts.UI.PickPlanTask(ctx, tasks)
	if err != nil {
		return store.Task{}, false, err
	}
	if !ok {
		return store.Task{}, false, nil
	}
	for _, t := range tasks {
		if t.ID == chosen {
			return t, true, nil
		}
	}
	return store.Task{}, false, fmt.Errorf("plan resume: task %q not found", chosen)
}

// resolveResumeByID loads the named task and validates it has a
// non-empty PlanResumeCursor. fs.ErrNotExist becomes the friendly
// "task %q not found" wrapping the way callers expect; an empty
// cursor becomes "task %q has no plan session".
func resolveResumeByID(stderr io.Writer, id string) (store.Task, bool, error) {
	s, ok := tasklog.OpenTaskLog(stderr)
	if !ok {
		return store.Task{}, false, errors.New("J: tasks database unavailable")
	}
	defer func() { _ = s.Close() }()
	task, err := s.GetTask(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return store.Task{}, false, fmt.Errorf("J: task %q not found", id)
		}
		return store.Task{}, false, err
	}
	if task.PlanResumeCursor == "" {
		return store.Task{}, false, fmt.Errorf("J: task %q has no plan session", id)
	}
	return task, true, nil
}

// listResumableTasks returns every task with a non-empty
// PlanResumeCursor (any status), sorted via store.SortTasks so the
// picker shows the active-then-most-recent order users see in
// `j tasks`. The bbolt store is closed before the slice is
// returned so the agent invocation downstream does not contend on
// the file lock.
func listResumableTasks(stderr io.Writer) ([]store.Task, error) {
	s, ok := tasklog.OpenTaskLog(stderr)
	if !ok {
		return nil, errors.New("J: tasks database unavailable")
	}
	defer func() { _ = s.Close() }()
	all, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	store.SortTasks(all)
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
func readBestEffortWarn(stderr io.Writer, path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "warning: read %s: %v\n", path, err)
		return ""
	}
	return string(data)
}

// planResumeBegin returns a copy of existing mutated for the resume
// case: status -> planning, PlanEndAt cleared so the finalize step
// stamps a fresh value, but the original PlanBeginAt is preserved
// exactly as the plan asks (a fresh timestamp is only minted when
// the existing row had none, e.g. for partial-state task rows).
// Tool/model/PlanResumeCursor are kept verbatim because resume by
// definition reuses them.
func planResumeBegin(existing store.Task) store.Task {
	task := existing
	task.Status = store.StatusPlanning
	task.PlanEndAt = nil
	if task.PlanBeginAt == nil {
		begin := time.Now().UTC()
		task.PlanBeginAt = &begin
	}
	return task
}

// planResumeFinish stamps the terminal state on a resume run. On
// success the status flips to plan-done and the summary is
// re-derived from the (possibly refined) requirements / plan
// markdown so the row reflects the latest body. On failure the
// status flips to help and the summary is left as-is so users can
// still recognise the row in `j tasks`.
func planResumeFinish(task store.Task, runErr error, refinedRequirements, planMarkdown, target string) store.Task {
	end := time.Now().UTC()
	task.PlanEndAt = &end
	if runErr != nil {
		task.Status = store.StatusHelp
		return task
	}
	task.Status = store.StatusPlanDone
	task.Summary = tasklog.Summary(tasklog.PickSource(refinedRequirements, planMarkdown), target)
	return task
}

// resolveResumeInteractive returns the planner bucket's stored
// `interactive` value, falling back to true when the bucket has
// no usable entry. Resume intentionally has no `--interactive`
// flag: the stored value is authoritative so users do not have to
// re-supply the choice on every resume. Never writes the bucket.
func resolveResumeInteractive(opts ResumeOptions) bool {
	if v, ok := storedResumeInteractive(opts); ok {
		return v
	}
	return true
}

// storedResumeInteractive looks up the planner bucket's
// `interactive` value. The settings DB is opened and closed solely
// for this read so the lock is not held across the agent call. A
// failed open or a missing / unparseable value yields (_, false)
// so callers fall back to the default.
func storedResumeInteractive(opts ResumeOptions) (bool, bool) {
	s, ok := openSettingsStore(opts.Stderr)
	if !ok {
		return false, false
	}
	defer func() { _ = s.Close() }()
	return agentpick.StoredInteractive(s, store.BucketPlanner)
}

// newResumeCmd builds the `j plan resume` cobra subcommand. It
// owns its own --from-task flag and the matching viper / env
// bindings so the parent `j plan` Run is unchanged.
//
// viper.BindPFlag and viper.BindEnv only fail when their input is
// nil or empty — programmer errors that this function does not
// produce — so their returned errors are intentionally discarded
// (mirroring the parent New).
func newResumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume a previously started plan session",
		Long: "Lists tasks whose plan session is non-empty and resumes the chosen one " +
			"using the tool/model recorded on the task row. " +
			"Pass --from-task <id> (or PLAN_RESUME_FROM_TASK) to skip the picker. " +
			"With no eligible sessions, prints `J: there are no resumable sessions` " +
			"and exits 0. " +
			"Resume reads `interactive` from the planner bucket and falls back to " +
			"true when unset; there is no `--interactive` flag because the stored " +
			"value is authoritative across resume runs.",
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
