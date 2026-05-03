package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/preflight"
	"github.com/spacelions/j/internal/cli/tasklog"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/util/mdfile"
	"github.com/spacelions/j/internal/util/run"
)

// StartOptions configures RunStart. Stdin/Stdout/Stderr default to the
// process streams; Agents must be supplied by the caller (the cobra
// wiring injects `[]codingagents.Agent{cursor.New(), claude.New()}`,
// tests inject scripted ones); Selector defaults to a huh-backed
// adapter so the agent-pick prompts can run on a real terminal.
type StartOptions struct {
	// FromFile is the markdown task description path. Required:
	// `j tasks start` runs detached so there is no terminal for the
	// markdown source picker.
	FromFile string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	// Selector is the agent-pick UI used by EnsureAgentSelections to
	// prompt for any missing planner / worker / verifier bucket.
	Selector AgentSelector

	// JBinary is the absolute path to the j binary re-executed as
	// `j tasks orchestrate --id <id>`. Empty falls back to
	// os.Executable. Tests inject a path-resolvable stub.
	JBinary string
}

// RunStart implements `j tasks start`. It mints a fresh task id,
// stages the user's markdown into <cwd>/.j/tasks/<id>/requirements.md,
// seeds the bbolt task row at status `planning`, and forks a detached
// `j tasks orchestrate --id <id>` subprocess whose stdout/stderr are
// appended to <cwd>/.j/tasks/<id>/agent.log. The detached child drives
// planner → worker → verifier end to end; RunStart records the child's
// PID on the task row and returns immediately so the user gets their
// shell prompt back.
//
// Steps:
//  1. Defer a huh.ErrUserAborted → nil guard so a Ctrl-C in the
//     agent-pick prompt exits cleanly.
//  2. Call EnsureAgentSelections so every bucket (planner, worker,
//     verifier) carries a tool/model pair before the orchestrator
//     fires. Already-populated buckets are no-ops; missing buckets
//     prompt once each. The bucket-stored `interactive` value is
//     never consulted by this command and never written here.
//  3. Resolve --from-file (required), read the user's markdown.
//  4. Mint a task id, EnsureTaskDir, write requirements.md.
//  5. Seed the task row with Status=planning + AgentLogPath.
//  6. Spawn the detached orchestrator. Record BackgroundPID.
//  7. Print "task <id> started; tail -f <agent.log>" and return.
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
	if opts.FromFile == "" {
		return errors.New("J: --from-file is required (j tasks start runs detached and cannot prompt)")
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

	source, body, err := readStartSource(opts.FromFile)
	if err != nil {
		return err
	}
	taskID := store.NewTaskID()
	taskDir, err := store.EnsureTaskDir(taskID)
	if err != nil {
		return fmt.Errorf("J: ensure task dir: %w", err)
	}
	requirementsPath := filepath.Join(taskDir, store.RequirementsFileName)
	if err := os.WriteFile(requirementsPath, []byte(body), 0o644); err != nil {
		return fmt.Errorf("J: stage requirements: %w", err)
	}
	agentLogPath := filepath.Join(taskDir, tasklog.AgentLogFileName)

	binary, err := resolveJBinary(opts.JBinary)
	if err != nil {
		return err
	}
	pid, err := run.SpawnIn(ctx, "", agentLogPath, binary, "tasks", "orchestrate", "--id", taskID)
	if err != nil {
		return err
	}
	begin := time.Now().UTC()
	tasklog.PersistWarn(opts.Stderr, store.Task{
		ID:            taskID,
		Status:        store.StatusPlanning,
		Summary:       tasklog.Summary(body, source),
		PlanBeginAt:   &begin,
		AgentLogPath:  agentLogPath,
		BackgroundPID: pid,
	})

	fmt.Fprintf(opts.Stdout, "J: task %s started; tail -f %s\n", taskID, agentLogPath)
	return nil
}

// readStartSource resolves the --from-file path and reads the body
// once so RunStart's downstream calls (writeFile to requirements.md,
// summary derivation) operate on a single in-memory copy.
func readStartSource(raw string) (string, string, error) {
	abs, err := mdfile.Resolve(raw)
	if err != nil {
		return "", "", err
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return "", "", fmt.Errorf("J: read source: %w", err)
	}
	return abs, string(body), nil
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
		o.Selector = newHuhAgentSelector(o.Stdin, o.Stderr)
	}
	return o
}

// newStartCmd builds the `j tasks start` cobra subcommand. The flag
// surface is just --from-file; the orchestrator runs detached so
// there is no terminal for additional pickers. The bucket-stored
// `interactive` value is never consulted on this path: the
// orchestrator forces Interactive=false internally for plan / work /
// verify when it shells out, leaving the bucket value untouched
// (manual `j plan|work|verify` continue to honour it).
func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new task: drive planner → worker → verifier in the background",
		Long: "Validates that every agent bucket (planner, worker, verifier) " +
			"has a tool/model selection — prompting once per missing bucket — " +
			"then forks a detached `j tasks orchestrate --id <id>` child " +
			"that drives planner → worker → verifier end to end and exits. " +
			"The user's markdown is staged into <cwd>/.j/tasks/<id>/requirements.md " +
			"before the spawn; every line written by the orchestrator and the " +
			"per-phase coding-agent children appends to the same per-task " +
			"<cwd>/.j/tasks/<id>/agent.log. Pass --from-file/-f (or " +
			"TASKS_START_FROM_FILE) to point at the markdown task description.",
		PersistentPreRunE: preflight.PreRunE,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunStart(cmd.Context(), StartOptions{
				FromFile: viper.GetString("tasks.start.from_file"),
				Stdin:    cmd.InOrStdin(),
				Stdout:   cmd.OutOrStdout(),
				Stderr:   cmd.ErrOrStderr(),
				Agents:   []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().StringP("from-file", "f", "", "Path to a markdown file describing the task")
	_ = viper.BindPFlag("tasks.start.from_file", cmd.Flags().Lookup("from-file"))
	_ = viper.BindEnv("tasks.start.from_file", "TASKS_START_FROM_FILE")
	return cmd
}
