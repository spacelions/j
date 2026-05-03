package tasks

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/plan"
	"github.com/spacelions/j/internal/cli/preflight"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
)

// StartOptions configures RunStart. Stdin/Stdout/Stderr default to the
// process streams; Agents must be supplied by the caller (the cobra
// wiring injects `[]codingagents.Agent{cursor.New(), claude.New()}`,
// tests inject scripted ones); Selector defaults to a huh-backed
// adapter so the agent-pick prompts can run on a real terminal.
type StartOptions struct {
	// FromFile is the markdown task description path forwarded to
	// plan.Run as plan.Options.FromFile. Empty triggers plan's own
	// source picker (markdown | linear) followed by the markdown
	// basename picker.
	FromFile string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	// Selector is the agent-pick UI used by EnsureAgentSelections to
	// prompt for any missing planner / worker / verifier bucket. The
	// markdown source / file pickers stay owned by plan.Run, so this
	// command does not need plan's full UI surface.
	Selector AgentSelector
}

// RunStart implements `j tasks start`. It is a thin wrapper that:
//
//  1. Defers a huh.ErrUserAborted -> nil guard so a Ctrl-C in any
//     downstream prompt exits cleanly.
//  2. Calls EnsureAgentSelections so every bucket (planner, worker,
//     verifier) carries a tool/model pair before the planner runs.
//     Already-populated buckets are no-ops; missing buckets prompt
//     once each.
//  3. Builds a plan.Options and calls plan.Run. plan.Run owns the
//     source picker (markdown | linear), the markdown file picker,
//     the planner bucket-or-pick chokepoint, and the agent.Plan
//     invocation; we intentionally do not reproduce any of that
//     here.
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
	return plan.Run(ctx, plan.Options{
		FromFile: opts.FromFile,
		Stdin:    opts.Stdin,
		Stdout:   opts.Stdout,
		Stderr:   opts.Stderr,
		Agents:   opts.Agents,
	})
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
// surface mirrors `j plan`'s --from-file only; the interactive mode is
// always read from the planner bucket (set via `j plan --interactive=...`)
// so the user's stored choice is authoritative. viper.BindPFlag /
// viper.BindEnv only fail on programmer errors so their returned errors
// are intentionally discarded.
func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new task: validate agent picks then run the planner",
		Long: "Validates that every agent bucket (planner, worker, verifier) " +
			"has a tool/model selection — prompting once per missing bucket — " +
			"then delegates to `j plan` to produce <cwd>/.j/tasks/<id>/" +
			"requirements.md and plan.md. Pass --from-file/-f (or " +
			"TASKS_START_FROM_FILE) to skip the source picker; otherwise " +
			"the planner shows the markdown | linear source picker followed " +
			"by the markdown basename picker. The planner agent's interactive " +
			"mode is read from the planner bucket (set it via " +
			"`j plan --interactive=false` if you want headless runs).",
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
