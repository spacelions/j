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
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
)

// noActiveWorkSessionMessage is printed when no task has a non-empty
// WorkResumeSession.
const noActiveWorkSessionMessage = "J: no tasks with an active work session"

// ResumeWorkOptions configures RunResumeWork.
type ResumeWorkOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     UI

	JBinary string
}

func (o ResumeWorkOptions) withDefaults() ResumeWorkOptions {
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

// RunResumeWork implements `j tasks resume-work`. It filters to tasks
// with a non-empty WorkResumeSession and re-execs the orchestrator inline.
func RunResumeWork(ctx context.Context, opts ResumeWorkOptions) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()

	taskID, ok, err := resolveResumeWorkTaskID(ctx, opts)
	if err != nil || !ok {
		return err
	}
	t, err := resolver.TaskByID(taskID)
	if err != nil {
		return err
	}
	if !tasks.IsLegal(t.Status, tasks.EventWorkResume) {
		return fmt.Errorf("J: cannot resume-work task in status %q", t.Status)
	}
	if _, err := tasks.EnsureDir(taskID); err != nil {
		return err
	}
	return runInlineOrchestrator(ctx, opts.JBinary, []string{
		"tasks", "orchestrate",
		"--id", taskID,
		"--phase=from-work",
		"--interactive=true",
	})
}

func resolveResumeWorkTaskID(ctx context.Context, opts ResumeWorkOptions) (string, bool, error) {
	s, err := tasks.OpenDefault()
	if err != nil {
		return "", false, err
	}
	id, ok, err := pickResumeWorkFromStore(ctx, s, opts)
	_ = s.Close()
	return id, ok, err
}

func pickResumeWorkFromStore(ctx context.Context, s *tasks.Store, opts ResumeWorkOptions) (string, bool, error) {
	rows, err := s.ListTasks()
	if err != nil {
		return "", false, err
	}
	filtered := filterTasksWithWorkSession(rows)
	if len(filtered) == 0 {
		uitheme.NormalFprintln(opts.Stdout, noActiveWorkSessionMessage)
		return "", false, nil
	}
	tasks.SortTasks(filtered)
	return opts.UI.PickTask(ctx, filtered)
}

func filterTasksWithWorkSession(rows []tasks.Task) []tasks.Task {
	out := make([]tasks.Task, 0, len(rows))
	for _, t := range rows {
		if t.WorkResumeSession != "" {
			out = append(out, t)
		}
	}
	return out
}

// newResumeWorkCmd builds the `j tasks resume-work` cobra subcommand.
func newResumeWorkCmd() *cobra.Command {
	agents := []codingagents.Agent{cursor.New(), claude.New()}
	cmd := &cobra.Command{
		Use:   "resume-work",
		Short: "Resume an in-flight worker session in the foreground with --interactive=true",
		Long: "Filters tasks to rows with a non-empty work_resume_session and " +
			"renders the shared task picker. The selected task re-execs " +
			"`j tasks orchestrate --phase=from-work --interactive=true` " +
			"inline so the worker resumes its session in the foreground with the " +
			"parent's terminal attached. When no task carries an active work session, " +
			"prints `J: no tasks with an active work session` and exits 0.",
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
			return RunResumeWork(cmd.Context(), ResumeWorkOptions{
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: agents,
			})
		},
	}
	return cmd
}
