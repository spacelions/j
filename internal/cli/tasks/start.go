package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/preflight"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/util/mdfile"
	"github.com/spacelions/j/internal/util/run"
)

// StartUI is the slice of picker methods RunStart drives when
// `--from-file` is empty: SelectSource (markdown | linear | task),
// PickMarkdownInCwd (markdown branch), PickTask (re-plan branch).
// *picker.Picker satisfies this surface; tests inject a scripted
// fake.
type StartUI interface {
	SelectSource(ctx context.Context, allowed []picker.Source) (picker.Source, error)
	PickMarkdownInCwd(ctx context.Context) (string, error)
	PickTask(ctx context.Context, title string, tasks []store.Task) (string, bool, error)
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

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	// Selector is the agent-pick UI used by EnsureAgentSelections to
	// prompt for any missing planner / worker / verifier bucket.
	Selector AgentSelector
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

// startTarget is the resolved outcome of resolveStartTarget: which
// task to chain against, plus any per-source side-information the
// seed step needs.
type startTarget struct {
	// taskID is the task this RunStart will spawn the orchestrator
	// against. Empty means "exit cleanly with no spawn" (linear
	// branch or aborted picker).
	taskID string
	// isNew distinguishes a freshly minted task (markdown source,
	// requirements.md needs writing, fresh row needs persisting)
	// from an existing one (re-plan source, no file writes, just
	// stamp PID + AgentLogPath onto the existing row).
	isNew bool
	// body is the markdown bytes to write to <task-dir>/requirements.md.
	// Set only when isNew is true.
	body string
	// source is the absolute path of the user's markdown source. Used
	// for summary derivation. Set only when isNew is true.
	source string
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
//  2. Call EnsureAgentSelections so every bucket has a tool/model
//     pair before the orchestrator fires.
//  3. resolveStartTarget: branch on FromFile (markdown new) or
//     UI.SelectSource (markdown new | task re-plan | linear no-op).
//  4. For new tasks: EnsureTaskDir + write requirements.md.
//     For re-plans: load the existing row.
//  5. Spawn the detached orchestrator. Record BackgroundPID +
//     AgentLogPath on the row.
//  6. Print the bordered two-line background-fork banner (subject
//     + PID on row one, `tail -f <agent.log>` on row three) via
//     banner.RunningInBackground and return.
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
	if err := EnsureAgentSelections(ctx, AgentCheckOptions{
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
	if target.taskID == "" {
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

	binary, err := resolveJBinary(opts.JBinary)
	if err != nil {
		return err
	}
	pid, err := run.SpawnIn(ctx, "", agentLogPath, binary,
		"tasks", "orchestrate",
		"--id", target.taskID,
		"--plan-requires-approval", strconv.FormatBool(planRequiresApproval),
	)
	if err != nil {
		return err
	}
	persistStartRow(opts.Stderr, target, agentLogPath, pid)

	banner.RunningInBackground(opts.Stdout, fmt.Sprintf("task %s", target.taskID), pid, agentLogPath)
	return nil
}

// resolveStartTarget decides whether RunStart spawns the orchestrator
// against a freshly minted task (markdown source) or an existing task
// (re-plan source), or exits cleanly (linear source / aborted picker).
//
//   - opts.FromFile != "" → markdown shortcut (mint new task).
//   - opts.FromFile == "" → picker.PickSource composite drives the
//     source widget + sub-picker; the switch below dispatches on the
//     resolved Source.
func resolveStartTarget(ctx context.Context, opts StartOptions) (startTarget, error) {
	if opts.FromFile != "" {
		return newTargetFromMarkdown(opts.FromFile)
	}
	res, err := picker.PickSource(ctx, opts.UI,
		[]picker.Source{picker.SourceMarkdown, picker.SourceLinear, picker.SourceTask},
		listAllTasks,
		errors.New("tasks: no tasks to re-plan; run `j tasks start --from-file <md>` first"))
	if err != nil {
		return startTarget{}, err
	}
	if res.Cancelled {
		return startTarget{}, nil
	}
	switch res.Source {
	case picker.SourceMarkdown:
		return newTargetFromMarkdown(res.Markdown)
	case picker.SourceTask:
		return startTarget{taskID: res.TaskID, isNew: false}, nil
	case picker.SourceLinear:
		fmt.Fprintln(opts.Stdout, "tasks: linear source is not yet wired up; nothing to do")
		return startTarget{}, nil
	}
	return startTarget{}, fmt.Errorf("tasks: unsupported source %s", res.Source)
}

// newTargetFromMarkdown reads the markdown body once and packages it
// into a startTarget for the new-task branch. Mints the task ID here
// so callers see a populated target on success.
func newTargetFromMarkdown(raw string) (startTarget, error) {
	abs, err := mdfile.Resolve(raw)
	if err != nil {
		return startTarget{}, err
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return startTarget{}, fmt.Errorf("J: read source: %w", err)
	}
	return startTarget{
		taskID: store.NewTaskID(),
		isNew:  true,
		body:   string(body),
		source: abs,
	}, nil
}

// listAllTasks opens the per-project tasks bbolt store, reads every
// row, sorts via store.SortTasks, and closes before returning. The
// settings store is closed before the picker runs so the bbolt file
// lock is not held across the long-running prompt.
func listAllTasks() ([]store.Task, error) {
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		return nil, fmt.Errorf("tasks: tasks db: %w", err)
	}
	s, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("tasks: tasks db: %w", err)
	}
	defer func() { _ = s.Close() }()
	all, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	store.SortTasks(all)
	return all, nil
}

// prepareTaskFiles ensures the per-task directory exists and, for
// new tasks, stages requirements.md from the in-memory body.
// Returns the absolute path to the per-task agent.log so the caller
// can hand it to run.SpawnIn. For re-plan targets, requirements.md
// is left untouched — the user is re-planning against the existing
// requirements.
func prepareTaskFiles(target startTarget) (string, error) {
	taskDir, err := store.EnsureTaskDir(target.taskID)
	if err != nil {
		return "", fmt.Errorf("J: ensure task dir: %w", err)
	}
	if target.isNew {
		requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
		if err := os.WriteFile(requirementsPath, []byte(target.body), 0o644); err != nil {
			return "", fmt.Errorf("J: stage requirements: %w", err)
		}
	}
	return filepath.Join(taskDir, store.AgentLogFileName), nil
}

// persistStartRow records the spawned PID + AgentLogPath onto the
// task row. New tasks get a freshly built row at status `planning`;
// existing tasks (re-plan branch) get a read-modify-write that
// preserves every other field — the orchestrator's own per-phase
// lifecycle takes over from there.
func persistStartRow(stderr io.Writer, target startTarget, agentLogPath string, pid int) {
	if target.isNew {
		begin := time.Now().UTC()
		store.PersistWarn(stderr, store.Task{
			ID:            target.taskID,
			Status:        store.StatusPlanning,
			Summary:       store.Summary(target.body, target.source),
			PlanBeginAt:   &begin,
			AgentLogPath:  agentLogPath,
			BackgroundPID: pid,
		})
		return
	}
	// Re-plan: load + mutate + put back. Best-effort; a missing
	// row would surface as a tasklog warning on stderr but the
	// detached child is already running, so we don't rollback.
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		fmt.Fprintf(stderr, "warning: tasks path: %v\n", err)
		return
	}
	s, err := store.Open(path)
	if err != nil {
		fmt.Fprintf(stderr, "warning: tasks db: %v\n", err)
		return
	}
	defer func() { _ = s.Close() }()
	row, err := s.GetTask(target.taskID)
	if err != nil {
		fmt.Fprintf(stderr, "warning: tasks get %q: %v\n", target.taskID, err)
		return
	}
	row.AgentLogPath = agentLogPath
	row.BackgroundPID = pid
	if err := s.PutTask(row); err != nil {
		fmt.Fprintf(stderr, "warning: tasks put: %v\n", err)
	}
}

// resolveJBinary returns the absolute path of the j binary the
// detached orchestrator child re-execs. An explicit override (via
// StartOptions.JBinary, used by tests) wins; otherwise os.Executable
// resolves the running binary so `j tasks start` self-execs the same
// j the user just invoked.
func resolveJBinary(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("J: resolve j binary: %w", err)
	}
	return exe, nil
}

func resolvePlanRequiresApproval(override *bool) (bool, error) {
	if override != nil {
		return *override, nil
	}
	return store.LoadPlanRequiresApproval()
}

func (o StartOptions) withDefaults() StartOptions {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.Selector == nil {
		o.Selector = picker.New(o.Stdin, o.Stderr)
	}
	if o.UI == nil {
		o.UI = picker.New(o.Stdin, o.Stderr)
	}
	return o
}

// newStartCmd builds the `j tasks start` cobra subcommand. The flag
// surface is --from-file plus the plan-approval override; when empty,
// the same source picker
// `j plan` shows is rendered against the parent's terminal before
// the detached fork. The bucket-stored `interactive` value is never
// consulted on this path: the orchestrator forces Interactive=false
// internally for plan / work / verify when it shells out, leaving
// the bucket value untouched (manual `j plan|work|verify` continue
// to honour it).
func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new task: drive planner, then pause for approval or continue in the background",
		Long: "Validates that every agent bucket (planner, worker, verifier) " +
			"has a tool/model selection — prompting once per missing bucket — " +
			"then forks a detached `j tasks orchestrate --id <id>` child that " +
			"runs the planner and, when plan approval is not required, drives " +
			"worker → verifier end to end before exiting. Pass " +
			"--from-file/-f (or TASKS_START_FROM_FILE) to point at a markdown " +
			"task description; when neither is set, the same source picker " +
			"`j plan` shows is rendered (markdown | linear | existing " +
			"task). After the spawn, a bordered two-line banner is " +
			"printed (subject + PID on row one, `tail -f <agent.log>` on row " +
			"three) so the user can follow along. Every line written by the " +
			"orchestrator and the per-phase coding-agent children appends to " +
			"the same per-task <cwd>/.j/tasks/<id>/agent.log.",
		PersistentPreRunE: preflight.PreRunE,
		RunE: func(cmd *cobra.Command, _ []string) error {
			approval, err := startPlanRequiresApprovalOverride(cmd)
			if err != nil {
				return err
			}
			return RunStart(cmd.Context(), StartOptions{
				FromFile:             viper.GetString("tasks.start.from_file"),
				PlanRequiresApproval: approval,
				Stdin:                cmd.InOrStdin(),
				Stdout:               cmd.OutOrStdout(),
				Stderr:               cmd.ErrOrStderr(),
				Agents:               []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().StringP("from-file", "f", "", "Path to a markdown file describing the task")
	cmd.Flags().Bool("plan-requires-approval", false, "Override project.plan_requires_approval for this run (use =false to skip once)")
	_ = viper.BindPFlag("tasks.start.from_file", cmd.Flags().Lookup("from-file"))
	_ = viper.BindEnv("tasks.start.from_file", "TASKS_START_FROM_FILE")
	_ = viper.BindPFlag("tasks.start.plan_requires_approval", cmd.Flags().Lookup("plan-requires-approval"))
	_ = viper.BindEnv("tasks.start.plan_requires_approval", "TASKS_START_PLAN_REQUIRES_APPROVAL")
	return cmd
}

func startPlanRequiresApprovalOverride(cmd *cobra.Command) (*bool, error) {
	approvalSet := cmd.Flags().Changed("plan-requires-approval") || envSet("TASKS_START_PLAN_REQUIRES_APPROVAL")
	if approvalSet {
		v := viper.GetBool("tasks.start.plan_requires_approval")
		return &v, nil
	}
	return nil, nil
}

func envSet(name string) bool {
	_, ok := os.LookupEnv(name)
	return ok
}
