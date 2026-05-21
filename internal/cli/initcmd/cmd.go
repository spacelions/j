// Package initcmd implements the `j init` subcommand. It owns the
// only write path that creates `<cwd>/.j/`, `.j/tasks/`, `.j/settings`,
// and `.j/tasks/list.db`. Every other j command relies on the
// pre-flight helper to assert this layout already exists; init is
// the single chokepoint where the bytes hit disk.
//
// The package is named initcmd (not init) because `init` is a
// reserved Go identifier reserved for package init functions.
package initcmd

import (
	"context"
	"io"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
)

// Options configures Run. Stdin/Stdout/Stderr default to the process
// streams. UI defaults to the huh-backed implementation; tests pass
// a scripted fake.
type Options struct {
	// Yes, when true, skips the confirm-reset prompt and proceeds to
	// the wipe-and-recreate path. Sourced from the --yes/-y flag and
	// the init.yes viper key.
	Yes bool

	// MustRead, when non-nil, pre-seeds project.must_read with the
	// pointed-to string verbatim (case-preserved, including the empty
	// string). nil leaves the key unset so the next preflight-gated
	// command surfaces the "Files every agent must read first" prompt
	// as before. Sourced from the --must-read CLI flag; the pointer
	// distinguishes "flag absent" from "flag set to empty value".
	MustRead *string

	// PlanRequiresApproval, when non-nil, seeds
	// project.plan_requires_approval with the pointed-to bool. nil
	// uses store.DefaultPlanRequiresApproval.
	PlanRequiresApproval *bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	UI UI
}

// New returns the `j init` cobra subcommand. viper bindings only fail
// on programmer error (nil flag) so the returned errors are
// intentionally discarded.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the .j/ project layout in the current directory",
		Long: "Creates the per-project state layout under <cwd>/: " +
			".j/, .j/tasks/, .j/settings, .j/tasks/list.db. When all " +
			"four artifacts already exist, init renders a confirmation " +
			"prompt (Enter / `y` accepts; anything else aborts); on " +
			"accept it removes .j/ and recreates the layout. On a " +
			"partial layout (some artifacts present) it fills in the " +
			"missing pieces without prompting. The --yes/-y flag " +
			"skips the prompt and always wipes-and-recreates.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := Options{
				Yes:    viper.GetBool("init.yes"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			}
			if cmd.Flags().Changed("must-read") {
				v, _ := cmd.Flags().GetString("must-read")
				opts.MustRead = &v
			}
			if cmd.Flags().Changed("plan-requires-approval") ||
				envSet("INIT_PLAN_REQUIRES_APPROVAL") {
				v := viper.GetBool("init.plan_requires_approval")
				opts.PlanRequiresApproval = &v
			}
			return Run(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolP(
		"yes", "y", false,
		"Skip the confirmation prompt and recreate the layout",
	)
	cmd.Flags().String(
		"must-read", "",
		`Pre-seed project.must_read (skip the preflight prompt). `+
			`Use --must-read="" to seed an empty value.`,
	)
	cmd.Flags().Bool(
		"plan-requires-approval",
		store.DefaultPlanRequiresApproval,
		"Seed project.plan_requires_approval",
	)
	_ = viper.BindPFlag("init.yes", cmd.Flags().Lookup("yes"))
	_ = viper.BindEnv("init.yes", "INIT_YES")
	_ = viper.BindPFlag(
		"init.plan_requires_approval",
		cmd.Flags().Lookup("plan-requires-approval"),
	)
	_ = viper.BindEnv(
		"init.plan_requires_approval",
		"INIT_PLAN_REQUIRES_APPROVAL",
	)
	return cmd
}

// Run executes `j init`. The state machine is:
//
//  1. Check ProjectInitialized.
//  2. If true and --yes is unset, prompt; on decline print
//     "init aborted" and return nil.
//  3. If the project is initialized (and the user accepted, or --yes
//     was set), os.RemoveAll the .j directory.
//  4. Run store.EnsureProject so the four artifacts (re)appear.
//  5. Print "initialized <abs-path>" to stdout.
//
// Tests cover each branch via the UI fake.
func Run(ctx context.Context, opts Options) error {
	opts = opts.withDefaults()
	initialized, err := store.ProjectInitialized()
	if err != nil {
		return err
	}
	if initialized && !opts.Yes {
		ok, err := opts.UI.ConfirmReset(ctx)
		if err != nil {
			return err
		}
		if !ok {
			uitheme.DangerousFprintln(opts.Stdout, "J: init aborted")
			return nil
		}
	}
	if initialized {
		_ = os.RemoveAll(store.DefaultDir())
	}
	if err := store.EnsureProject(); err != nil {
		return err
	}
	_ = seedDefaults(opts.MustRead, opts.PlanRequiresApproval)
	jDir := store.DefaultDir()
	uitheme.NormalFprintf(opts.Stdout, "J: initialized %s\n", jDir)
	return nil
}

// defaultMaxIterations is the seed value written to
// project.max_iterations on every successful `j init`. The user can
// override it later via `j settings set project.max_iterations=...`.
const defaultMaxIterations = "3"

// seedDefaults opens the freshly-created settings store once and
// writes the project-bucket defaults: max_iterations is always
// reseeded to defaultMaxIterations, plan_requires_approval is always
// reseeded, and must_read is persisted
// verbatim when the caller passed --must-read (mustRead != nil). A
// nil mustRead leaves the key unset so the next preflight-gated
// command surfaces the "Files every agent must read first" prompt.
// The empty string is stored as-is to honour the "blank input is
// valid" contract.
func seedDefaults(mustRead *string, planRequiresApproval *bool) error {
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	_ = s.Put(store.BucketProject, store.KeyMaxIterations, defaultMaxIterations)
	approval := store.DefaultPlanRequiresApproval
	if planRequiresApproval != nil {
		approval = *planRequiresApproval
	}
	_ = s.Put(
		store.BucketProject, store.KeyPlanRequiresApproval,
		strconv.FormatBool(approval))

	if mustRead != nil {
		_ = s.Put(store.BucketProject, resolver.KeyMustRead, *mustRead)
	}
	return nil
}

func envSet(name string) bool {
	_, ok := os.LookupEnv(name)
	return ok
}

func (o Options) withDefaults() Options {
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
