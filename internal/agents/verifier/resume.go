package verifier

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/util/run"
)

// ResumeOptions configures RunResume. Stdin/Stdout/Stderr default to
// the process streams; UI defaults to the huh implementation; Agents
// must be supplied by the caller (the cobra wiring injects every
// registered backend — cursor, claude, deepseek — tests inject
// scripted ones).
//
// TaskID short-circuits the selector: when non-empty Run loads that
// task directly. Otherwise Run lists every task whose
// VerifyResumeSession is non-empty (any status — explicitly NOT the
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
// task's existing VerifyResumeSession is reused (no fresh
// NewResumeID), the original VerifyBeginAt is preserved, and the
// eligibility filter is permissive — any task with a non-empty
// VerifyResumeSession qualifies, regardless of status.
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
		return errors.New("no coding agents configured")
	}
	t, ok, err := resolveResumeTask(ctx, opts)
	if err != nil {
		return err
	}
	if !ok {
		uitheme.NormalFprintln(
			opts.Stdout,
			"J: there are no resumable verify sessions",
		)
		return nil
	}
	if !tasks.IsLegal(t.Status, tasks.EventVerifyResume) {
		return fmt.Errorf(
			"cannot resume verify on task in status %q", t.Status,
		)
	}
	agent, ok := lookupResumeAgent(opts.Agents, t.VerifyTool)
	if !ok {
		return fmt.Errorf("unknown tool %q", t.VerifyTool)
	}
	return runVerifyResume(ctx, opts, t, agent)
}

// runVerifyResume drives the always-interactive verifier resume
// turn. Split out of RunResume so the entry point stays under the
// 80-line method cap while the picker / agent-lookup branch keeps
// its early-exit shape.
func runVerifyResume(
	ctx context.Context, opts ResumeOptions,
	t tasks.Task, agent codingagents.Agent,
) error {
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		return err
	}
	taskDir := filepath.Join(tasksDir, t.ID)
	planPath := filepath.Join(taskDir, tasks.PlanFileName)
	requirementsPath := filepath.Join(
		taskDir, tasks.RequirementsFileName,
	)
	verifierPlanPath := filepath.Join(
		taskDir, tasks.VerifierPlanFileName,
	)
	findingsPath := filepath.Join(
		taskDir, tasks.VerifierFindingsFileName,
	)
	clarificationPath := filepath.Join(
		taskDir, tasks.ClarificationFileName,
	)
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)

	lc := lifecycle.BeginVerifyResume(t, opts.Stderr, agentLogPath)
	mustReadFiles, mustReadErr := resolver.MustRead()
	if mustReadErr != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", mustReadErr)
	}
	pid, runErr := agent.Verify(ctx, codingagents.VerifyRequest{
		TaskDir:                    taskDir,
		RequirementsPath:           requirementsPath,
		PlanPath:                   planPath,
		VerifierPlanOutputPath:     verifierPlanPath,
		VerifierFindingsOutputPath: findingsPath,
		ClarificationPath:          clarificationPath,
		Model:                      t.VerifyModel,
		Interactive:                true,
		ResumeChatID:               t.VerifyResumeSession,
		Resume:                     true,
		ResumeFromClarification:    tasks.ClarificationFileExists(taskDir),
		MustRead:                   mustReadFiles,
	})
	if runErr == nil {
		runErr = run.WaitForExit(ctx, pid)
	}
	outcome := lifecycle.VerifyOutcomeNoRetries
	if runErr == nil &&
		resolver.ParseVerdict(findingsPath) == resolver.VerdictPass {
		outcome = lifecycle.VerifyOutcomeSuccess
	}
	lc.Finish(outcome, runErr)
	if runErr != nil {
		return runErr
	}
	uitheme.NormalFprintf(
		opts.Stdout, "J: verify resume on task %s\n", t.ID,
	)
	return nil
}

// resolveResumeTask runs the --from-task / picker / single-task
// short-circuit flow and returns the chosen task. The bool result
// is false (with nil error) when no eligible tasks exist; callers
// should print the "no resumable verify sessions" line and return
// nil.
func resolveResumeTask(
	ctx context.Context, opts ResumeOptions,
) (tasks.Task, bool, error) {
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
	chosen, ok, err := opts.UI.PickTask(
		ctx, "Select a task to resume verifying", rows,
	)
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
	return tasks.Task{}, false, fmt.Errorf("task %q not found", chosen)
}

// resolveResumeByID loads the named task and validates it has a
// non-empty VerifyResumeSession. fs.ErrNotExist becomes the friendly
// "task %q not found" wrapping; an empty cursor becomes
// "task %q has no verify session".
func resolveResumeByID(id string) (tasks.Task, bool, error) {
	t, err := resolver.TaskByID(id)
	if err != nil {
		return tasks.Task{}, false, err
	}
	if t.VerifyResumeSession == "" {
		return tasks.Task{}, false, fmt.Errorf("task %q has no verify session", id)
	}
	return t, true, nil
}

// listResumableTasks returns every task with a non-empty
// VerifyResumeSession regardless of status, sorted via
// tasks.SortTasks. validateForVerify is intentionally NOT applied
// here: resume is permissive by design.
func listResumableTasks() ([]tasks.Task, error) {
	all, err := resolver.ListAllTasks()
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, t := range all {
		if t.VerifyResumeSession != "" {
			out = append(out, t)
		}
	}
	return out, nil
}
