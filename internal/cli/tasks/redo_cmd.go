package tasks

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/preflight"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
)

// redoCmdSpec captures the per-phase wiring shared by
// `j tasks plan`, `j tasks work`, and `j tasks verify`. The factory
// `newRedoCmd` consumes a spec to produce the cobra command so the
// three subcommand files only declare their phase-specific labels
// and viper / env keys.
type redoCmdSpec struct {
	phase    redoPhase
	use      string
	short    string
	long     string
	viperKey string // e.g. "tasks.plan"
	envKey   string // e.g. "TASKS_PLAN"
}

// newRedoCmd builds a `j tasks <phase>` cobra subcommand from spec.
// Every phase shares the same flag surface (--from-task,
// --interactive), the same viper / env binding shape, and the same
// dispatch through runRedo, so this factory removes the per-file
// duplication and keeps the wiring single-sourced.
//
// viper.BindPFlag and viper.BindEnv only fail when their input is
// nil or empty — programmer errors that this function does not
// produce — so their returned errors are intentionally discarded.
func newRedoCmd(spec redoCmdSpec) *cobra.Command {
	fromTaskKey := spec.viperKey + ".from_task"
	interactiveKey := spec.viperKey + ".interactive"
	fromTaskEnv := spec.envKey + "_FROM_TASK"
	interactiveEnv := spec.envKey + "_INTERACTIVE"
	cmd := &cobra.Command{
		Use:               spec.use,
		Short:             spec.short,
		Long:              spec.long,
		PersistentPreRunE: preflight.PreRunE,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRedo(cmd.Context(), spec.phase, RedoOptions{
				TaskID:      viper.GetString(fromTaskKey),
				Interactive: resolveRedoInteractive(cmd, interactiveKey, interactiveEnv),
				Stdin:       cmd.InOrStdin(),
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
				Agents:      []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().String("from-task", "", "Re-enter the named task without showing the picker")
	cmd.Flags().Bool("interactive", true, "Launch the coding agent in interactive mode (its TUI). One-off override; not persisted.")
	_ = viper.BindPFlag(fromTaskKey, cmd.Flags().Lookup("from-task"))
	_ = viper.BindPFlag(interactiveKey, cmd.Flags().Lookup("interactive"))
	_ = viper.BindEnv(fromTaskKey, fromTaskEnv)
	_ = viper.BindEnv(interactiveKey, interactiveEnv)
	return cmd
}

// resolveRedoInteractive computes the per-call --interactive value
// using the same precedence as plan/work/verify cmd.go: explicit
// flag or env var > default true. Unlike resolver.Interactive there
// is no bucket fallback — these subcommands intentionally never
// persist `interactive` (the contract on `j tasks plan|work|verify`
// is one-off).
func resolveRedoInteractive(cmd *cobra.Command, viperKey, envName string) bool {
	if cmd.Flags().Changed("interactive") || os.Getenv(envName) != "" {
		return viper.GetBool(viperKey)
	}
	return true
}
