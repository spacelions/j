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
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

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

	// Mustread, when non-nil, pre-seeds project.mustread with the
	// pointed-to string verbatim (case-preserved, including the empty
	// string). nil leaves the key unset so the next preflight-gated
	// command surfaces the "Files every agent must read first" prompt
	// as before. Sourced from the --mustread CLI flag; the pointer
	// distinguishes "flag absent" from "flag set to empty value".
	Mustread *string

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
			if cmd.Flags().Changed("mustread") {
				v, _ := cmd.Flags().GetString("mustread")
				opts.Mustread = &v
			}
			return Run(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolP("yes", "y", false, "Skip the confirmation prompt and recreate the layout")
	cmd.Flags().String("mustread", "", `Pre-seed project.mustread (skip the preflight prompt). Use --mustread="" to seed an empty value.`)
	_ = viper.BindPFlag("init.yes", cmd.Flags().Lookup("yes"))
	_ = viper.BindEnv("init.yes", "INIT_YES")
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
			fmt.Fprintln(opts.Stdout, "init aborted")
			return nil
		}
	}
	if initialized {
		jDir, err := store.DefaultDir()
		if err != nil {
			return err
		}
		if err := os.RemoveAll(jDir); err != nil {
			return fmt.Errorf("init: remove %q: %w", jDir, err)
		}
	}
	if err := store.EnsureProject(); err != nil {
		return err
	}
	if err := seedDefaults(opts.Mustread); err != nil {
		return err
	}
	jDir, err := store.DefaultDir()
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.Stdout, "initialized %s\n", jDir)
	return nil
}

// defaultMaxIterations is the seed value written to
// project.max_iterations on every successful `j init`. The user can
// override it later via `j settings set project.max_iterations=...`.
const defaultMaxIterations = "3"

// seedDefaults opens the freshly-created settings store once and
// writes the project-bucket defaults: max_iterations is always
// reseeded to defaultMaxIterations, and mustread is persisted
// verbatim when the caller passed --mustread (mustread != nil). A
// nil mustread leaves the key unset so the next preflight-gated
// command surfaces the "Files every agent must read first" prompt.
// The empty string is stored as-is to honour the "blank input is
// valid" contract.
func seedDefaults(mustread *string) error {
	path, err := store.DefaultPath()
	if err != nil {
		return err
	}
	s, err := store.Open(path)
	if err != nil {
		return err
	}
	if err := s.Put(store.BucketProject, "max_iterations", defaultMaxIterations); err != nil {
		_ = s.Close()
		return fmt.Errorf("init: persist max_iterations: %w", err)
	}
	if mustread != nil {
		if err := s.Put(store.BucketProject, "mustread", *mustread); err != nil {
			_ = s.Close()
			return fmt.Errorf("init: persist mustread: %w", err)
		}
	}
	if err := s.Close(); err != nil {
		return fmt.Errorf("init: close store: %w", err)
	}
	return nil
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
