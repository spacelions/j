package verify

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
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/util/run"
)

// ResumeOptions configures RunResume. Stdin/Stdout/Stderr default to
// the process streams; UI defaults to the huh implementation; Agents
// must be supplied by the caller (the cobra wiring injects
// `[]codingagents.Agent{cursor.New(), claude.New()}`, tests inject scripted ones).
//
// TaskID short-circuits the selector: when non-empty Run loads that
// task directly. Otherwise Run lists every task whose
// VerifyResumeCursor is non-empty (any status — explicitly NOT the
// validateForVerify allowlist), prints
// "J: there are no resumable verify sessions" if the slice is
// empty, auto-selects the single eligible row when there is exactly
// one, and asks UI.PickVerifyTask when there are multiple.
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

// RunResume implements `j verify resume`. The lifecycle mirrors
// `j verify --from-task` reuse with three key differences: the
// task's existing VerifyResumeCursor is reused (no fresh
// NewResumeID), the original VerifyBeginAt is preserved, and the
// eligibility filter is permissive — any task with a non-empty
// VerifyResumeCursor qualifies, regardless of status.
//
// Resume always runs interactive — by definition resume is iterative
// (the user drives the next step from the TUI), and headless mode
// has no stdin path back to the human. The verifier bucket's
// `interactive` value is intentionally ignored on resume.
//
// As with runVerifyLoop, RunResume blocks on the spawned verifier
// child via run.WaitForExit before reading the findings file. The
// returned PID is always 0 on the always-interactive resume path
// (interactive backends run synchronously and do not detach), so
// WaitForExit is effectively a no-op; it stays in the call chain
// for symmetry with the first-run flow.
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

	task, ok, err := resolveResumeTask(ctx, opts)
	if err != nil {
		return err
	}
	if !ok {
		banner.Fprintln(opts.Stdout, "J: there are no resumable verify sessions")
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
	planPath := filepath.Join(taskDir, store.PlanFileName)
	requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
	verifierPlanPath := filepath.Join(taskDir, store.VerifierPlanFileName)
	findingsPath := filepath.Join(taskDir, store.VerifierFindingsFileName)

	lc := task.BeginVerifyResume(opts.Stderr)
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		banner.DangerousBox(opts.Stderr, "J: %v", mustReadErr)
	}
	// Resume always runs interactive — the verifier bucket's
	// `interactive` value is intentionally ignored on resume.
	// PID is always 0 here (interactive backends run synchronously
	// and do not detach), so WaitForExit below is a no-op; the call
	// is preserved for symmetry with the first-run flow.
	pid, runErr := agent.Verify(ctx, codingagents.VerifyRequest{
		RequirementsPath:           requirementsPath,
		PlanPath:                   planPath,
		VerifierPlanOutputPath:     verifierPlanPath,
		VerifierFindingsOutputPath: findingsPath,
		Model:                      task.InvokedModel,
		Interactive:                true,
		ResumeChatID:               task.VerifyResumeCursor,
		Resume:                     true,
		MustRead:                   mustReadFiles,
	})
	if runErr == nil {
		runErr = run.WaitForExit(ctx, pid)
	}
	outcome := store.VerifyOutcomeNoRetries
	if runErr == nil && resolver.ParseVerdict(findingsPath) == "PASS" {
		outcome = store.VerifyOutcomeSuccess
	}
	lc.Finish(outcome, runErr)
	if runErr != nil {
		return runErr
	}

	banner.Fprintf(opts.Stdout, "J: verify resume on task %s\n", task.ID)
	return nil
}

// resolveResumeTask runs the --from-task / picker / single-task
// short-circuit flow and returns the chosen task. The bool result
// is false (with nil error) when no eligible tasks exist; callers
// should print the "no resumable verify sessions" line and return
// nil.
func resolveResumeTask(ctx context.Context, opts ResumeOptions) (store.Task, bool, error) {
	if opts.TaskID != "" {
		return resolveResumeByID(opts.TaskID)
	}
	tasks, err := listResumableTasks()
	if err != nil {
		return store.Task{}, false, err
	}
	switch len(tasks) {
	case 0:
		return store.Task{}, false, nil
	case 1:
		return tasks[0], true, nil
	}
	chosen, ok, err := opts.UI.PickTask(ctx, "Select a task to resume verifying", tasks)
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
	return store.Task{}, false, fmt.Errorf("J: task %q not found", chosen)
}

// resolveResumeByID loads the named task and validates it has a
// non-empty VerifyResumeCursor. fs.ErrNotExist becomes the friendly
// "task %q not found" wrapping; an empty cursor becomes
// "task %q has no verify session".
func resolveResumeByID(id string) (store.Task, bool, error) {
	task, err := resolver.TaskByID("verify", id)
	if err != nil {
		return store.Task{}, false, err
	}
	if task.VerifyResumeCursor == "" {
		return store.Task{}, false, fmt.Errorf("J: task %q has no verify session", id)
	}
	return task, true, nil
}

// listResumableTasks returns every task with a non-empty
// VerifyResumeCursor regardless of status, sorted via
// store.SortTasks. validateForVerify is intentionally NOT applied
// here: resume is permissive by design.
func listResumableTasks() ([]store.Task, error) {
	all, err := resolver.ListAllTasks()
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, t := range all {
		if t.VerifyResumeCursor != "" {
			out = append(out, t)
		}
	}
	return out, nil
}

// newResumeCmd builds the `j verify resume` cobra subcommand.
//
// viper.BindPFlag and viper.BindEnv only fail when their input is
// nil or empty — programmer errors that this function does not
// produce — so their returned errors are intentionally discarded.
func newResumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume a previously started verify session",
		Long: "Lists tasks whose verify session is non-empty and resumes the chosen one " +
			"using the tool/model recorded on the task row. " +
			"Pass --from-task <id> (or VERIFY_RESUME_FROM_TASK) to skip the picker. " +
			"With no eligible sessions, prints `J: there are no resumable verify sessions` " +
			"and exits 0. The eligibility filter is intentionally permissive: tasks " +
			"in any status are resumable as long as their verify_resume_cursor is non-empty. " +
			"Resume always runs interactive; the verifier bucket's `interactive` value " +
			"is ignored.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunResume(cmd.Context(), ResumeOptions{
				TaskID: viper.GetString("verify.resume.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().String("from-task", "", "Resume the named task without showing the picker")
	_ = viper.BindPFlag("verify.resume.from_task", cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("verify.resume.from_task", "VERIFY_RESUME_FROM_TASK")
	return cmd
}
