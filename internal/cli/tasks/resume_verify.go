package tasks

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/cli/preflight"
	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
)

const noActiveVerifySessionMessage = "J: no tasks with an active verify session"

// ResumeVerifyOptions configures RunResumeVerify.
type ResumeVerifyOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI

	JBinary string
}

func (o ResumeVerifyOptions) withDefaults() ResumeVerifyOptions {
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

// RunResumeVerify implements `j tasks resume-verify`. It filters tasks
// with a non-empty VerifyResumeSession and re-execs
// `j tasks orchestrate --phase=verify-only --interactive=true` inline.
func RunResumeVerify(
	ctx context.Context, opts ResumeVerifyOptions,
) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()

	taskID, ok, err := resolveResumeVerifyTaskID(ctx, opts)
	if err != nil || !ok {
		return err
	}
	t, err := resolver.TaskByID(taskID)
	if err != nil {
		return err
	}
	if !tasks.IsLegal(t.Status, tasks.EventVerifyResume) {
		return fmt.Errorf("cannot resume-verify task in status %q", t.Status)
	}
	if _, err := tasks.EnsureDir(taskID); err != nil {
		return err
	}
	return runInlineOrchestrator(ctx, opts.JBinary, []string{
		cmdTasks, cmdOrchestrate,
		flagID, taskID,
		flagPhaseVerifyOnly,
		flagInteractiveTrue,
	})
}

func resolveResumeVerifyTaskID(
	ctx context.Context, opts ResumeVerifyOptions,
) (string, bool, error) {
	s, err := tasks.OpenDefault()
	if err != nil {
		return "", false, err
	}
	id, ok, err := pickResumeVerifyFromStore(ctx, s, opts)
	_ = s.Close()
	return id, ok, err
}

func pickResumeVerifyFromStore(
	ctx context.Context, s *tasks.Store, opts ResumeVerifyOptions,
) (string, bool, error) {
	rows, err := s.ListTasks()
	if err != nil {
		return "", false, err
	}
	filtered := filterTasksWithVerifySession(rows)
	if len(filtered) == 0 {
		uitheme.NormalFprintln(opts.Stdout, noActiveVerifySessionMessage)
		return "", false, nil
	}
	tasks.SortTasks(filtered)
	return opts.UI.PickTask(ctx, filtered)
}

func filterTasksWithVerifySession(rows []tasks.Task) []tasks.Task {
	out := make([]tasks.Task, 0, len(rows))
	for _, t := range rows {
		if t.VerifyResumeSession != "" {
			out = append(out, t)
		}
	}
	return out
}

// newResumeVerifyCmd builds the `j tasks resume-verify` cobra subcommand.
func newResumeVerifyCmd() *cobra.Command {
	agents := defaultAgents()
	cmd := &cobra.Command{
		Use:   "resume-verify",
		Short: "Resume an in-flight verifier session in the foreground",
		Long: "Filters tasks to rows with a non-empty verify_resume_session and " +
			"renders the shared task picker. The selected task re-execs " +
			"`j tasks orchestrate --phase=verify-only --interactive=true` " +
			"inline so the verifier resumes its session in the foreground. " +
			"When no task carries an active verify session, prints " +
			"`J: no tasks with an active verify session` and exits 0.",
		PersistentPreRunE: preflight.PreRunE,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return preflight.EnsureAgentSelections(
				cmd.Context(),
				preflight.AgentCheckOptions{
					Stdin:  cmd.InOrStdin(),
					Stdout: cmd.OutOrStdout(),
					Stderr: cmd.ErrOrStderr(),
					Agents: agents,
				})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunResumeVerify(cmd.Context(), ResumeVerifyOptions{
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: agents,
			})
		},
	}
	return cmd
}
