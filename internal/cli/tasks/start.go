package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/preflight"
	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/util/run"
)

// StartUI is the slice of picker methods RunStart drives when
// `--from-file` is empty: the source picker (markdown | linear |
// task), the markdown / re-plan / Linear sub-pickers. *picker.Picker
// satisfies this surface; tests inject a scripted fake.
type StartUI interface {
	SelectSource(ctx context.Context, allowed []picker.Source) (picker.Source, error)
	PickMarkdownInCwd(ctx context.Context) (string, error)
	PickTask(ctx context.Context, title string, tasks []tasks.Task) (string, bool, error)
	PromptLinearAPIKey(ctx context.Context, openURL string) (string, bool, error)
	PickLinearProject(ctx context.Context, projects []linear.Project) (linear.Project, bool, error)
	PromptLinearIdentifier(ctx context.Context) (string, bool, error)
}

// StartOptions configures RunStart. Stdin/Stdout/Stderr default to the
// process streams; Agents must be supplied by the caller (the cobra
// wiring injects `[]codingagents.Agent{cursor.New(), claude.New()}`,
// tests inject scripted ones); Selector defaults to a huh-backed
// adapter so the agent-pick prompts can run on a real terminal; UI
// defaults to picker.New so the source / file / re-plan pickers
// match `j plan` exactly.
type StartOptions struct {
	// FromFile is the markdown task description path. When set, the
	// source picker is skipped and the markdown branch fires
	// directly. When empty, RunStart drives UI.SelectSource.
	FromFile string
	// FromLinear is a Linear issue identifier. When set, RunStart
	// fetches the issue and stages requirements.md from the
	// rendered markdown without prompting; loses to FromFile if
	// both are set so the on-disk file always wins.
	FromLinear string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	// Selector is the agent-pick UI used by preflight.EnsureAgentSelections to
	// prompt for any missing planner / worker / verifier bucket.
	Selector preflight.AgentSelector
	// UI drives the source / file / re-plan pickers when FromFile is
	// empty. Defaults to picker.New.
	UI StartUI

	// JBinary is the absolute path to the j binary re-executed as
	// `j tasks orchestrate --id <id>`. Empty falls back to
	// os.Executable. Tests inject a path-resolvable stub.
	JBinary string

	// PlanRequiresApproval, when non-nil, overrides
	// project.plan_requires_approval for this start. nil means inherit
	// the project setting.
	PlanRequiresApproval *bool
}

type startTarget = resolver.StartTarget

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new task: drive planner, then pause for approval or continue in the background",
		Long: "Validates that every agent bucket (planner, worker, verifier) " +
			"has a tool/model selection, then forks a detached `j tasks orchestrate --id <id>` child. " +
			"Pass --from-file/-f (or TASKS_START_FROM_FILE) to point at a markdown task description; " +
			"without it, the same source picker `j plan` shows is rendered.",
		PersistentPreRunE: preflight.PreRunE,
		RunE: func(cmd *cobra.Command, _ []string) error {
			approval, err := startPlanRequiresApprovalOverride(cmd)
			if err != nil {
				return err
			}
			return RunStart(cmd.Context(), StartOptions{
				FromFile:             viper.GetString("tasks.start.from_file"),
				FromLinear:           viper.GetString("tasks.start.from_linear"),
				PlanRequiresApproval: approval,
				Stdin:                cmd.InOrStdin(),
				Stdout:               cmd.OutOrStdout(),
				Stderr:               cmd.ErrOrStderr(),
				Agents:               []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().StringP("from-file", "f", "", "Path to a markdown file describing the task")
	cmd.Flags().String("from-linear", "", "Linear issue identifier (e.g. ENG-123); requires linear.api_key in settings")
	cmd.Flags().Bool("plan-requires-approval", false, "Override project.plan_requires_approval for this run (use =false to skip once)")
	_ = viper.BindPFlag("tasks.start.from_file", cmd.Flags().Lookup("from-file"))
	_ = viper.BindEnv("tasks.start.from_file", "TASKS_START_FROM_FILE")
	_ = viper.BindPFlag("tasks.start.from_linear", cmd.Flags().Lookup("from-linear"))
	_ = viper.BindEnv("tasks.start.from_linear", "TASKS_START_FROM_LINEAR")
	_ = viper.BindPFlag("tasks.start.plan_requires_approval", cmd.Flags().Lookup("plan-requires-approval"))
	_ = viper.BindEnv("tasks.start.plan_requires_approval", "TASKS_START_PLAN_REQUIRES_APPROVAL")
	return cmd
}

// RunStart implements `j tasks start`. It mints (or re-uses) a task
// id, optionally stages the user's markdown into requirements.md,
// seeds the bbolt task row at status `planning` (or stamps the PID
// onto an existing row), and forks a detached
// `j tasks orchestrate --id <id>` subprocess whose stdout/stderr are
// appended to <cwd>/.j/tasks/<id>/agent.log. The detached child
// drives planner only when plan approval is required, otherwise
// planner → worker → verifier end to end. RunStart records the
// child's PID and returns immediately.
//
// Steps:
//  1. Defer a huh.ErrUserAborted → nil guard so a Ctrl-C in any
//     prompt exits cleanly.
//  2. Call preflight.EnsureAgentSelections so every bucket has a tool/model
//     pair before the orchestrator fires.
//  3. resolveStartTarget: branch on FromFile (markdown new) or
//     UI.SelectSource (markdown new | task re-plan | linear no-op).
//  4. For new tasks: EnsureTaskDir + write requirements.md.
//     For re-plans: load the existing row.
//  5. Spawn the detached orchestrator. Record BackgroundPID +
//     AgentLogPath on the row.
//  6. Print the bordered two-line background-fork banner (subject
//     + PID on row one, `tail -f <agent.log>` on row three) via
//     uitheme.NormalForkDialog and return.
func RunStart(ctx context.Context, opts StartOptions) (err error) {
	defer func() {
		if errors.Is(err, huh.ErrUserAborted) {
			err = nil
		}
	}()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("J: no coding agents configured")
	}
	if err := preflight.EnsureAgentSelections(ctx, preflight.AgentCheckOptions{
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
		Agents: opts.Agents,
		UI:     opts.Selector,
	}); err != nil {
		return err
	}

	target, err := resolveStartTarget(ctx, opts)
	if err != nil {
		return err
	}
	if target.TaskID == "" {
		// Linear source or aborted picker — exit cleanly.
		return nil
	}
	planRequiresApproval, err := resolvePlanRequiresApproval(opts.PlanRequiresApproval)
	if err != nil {
		return err
	}

	agentLogPath, err := prepareTaskFiles(target)
	if err != nil {
		return err
	}
	pid, err := spawnDetachedOrchestrator(ctx, opts.JBinary, agentLogPath, []string{
		"tasks", "orchestrate",
		"--id", target.TaskID,
		"--plan-requires-approval=" + strconv.FormatBool(planRequiresApproval),
	})
	if err != nil {
		return err
	}
	persistStartRow(opts.Stderr, target, agentLogPath, pid)
	uitheme.NormalForkDialog(opts.Stdout, fmt.Sprintf("task %s", target.TaskID), pid, agentLogPath)
	return nil
}

// spawnDetachedOrchestrator resolves the j binary, opens / re-uses
// the per-task agent.log via run.SpawnIn, and returns the spawned
// child's PID. Shared between `j tasks start` (planner-first spawn)
// and `j tasks continue` (resume-after-plan-done spawn).
func spawnDetachedOrchestrator(ctx context.Context, binaryOverride, agentLogPath string, args []string) (int, error) {
	binary, err := resolveJBinary(binaryOverride)
	if err != nil {
		return 0, err
	}
	return run.SpawnIn(ctx, "", agentLogPath, binary, args...)
}

func resolveStartTarget(ctx context.Context, opts StartOptions) (startTarget, error) {
	if opts.FromFile != "" {
		return resolver.NewStartTargetFromMarkdown(opts.FromFile)
	}
	if opts.FromLinear != "" {
		return resolver.StartTargetFromLinear(ctx, opts.FromLinear)
	}
	return resolver.ResolveStartTarget(ctx, opts.UI, opts.Stdout, "")
}

// prepareTaskFiles ensures the per-task directory exists and, for
// new tasks, stages requirements.md from the in-memory body.
// Returns the absolute path to the per-task agent.log so the caller
// can hand it to run.SpawnIn. For re-plan targets, requirements.md
// is left untouched — the user is re-planning against the existing
// requirements.
func prepareTaskFiles(target startTarget) (string, error) {
	return resolver.PrepareStartTaskFiles(target)
}
