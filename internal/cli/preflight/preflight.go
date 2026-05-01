// Package preflight is the shared pre-flight check used by every
// j subcommand that touches per-project state on disk. The exported
// helper Ensure verifies the .j layout is present, prompts the user
// to run `j init` when something is missing, and returns one of two
// sentinel errors so callers can distinguish "decline -> not
// initialized" from "accept -> please re-run your command".
//
// PreRunE wires Ensure into a cobra PersistentPreRunE so j plan,
// j work, j tasks, and j settings inherit the same behavior with
// one line of registration each.
package preflight

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/config"
	"github.com/spacelions/j/internal/store"
)

// ErrNeedsRetry is returned by Ensure when the user accepted the
// init prompt and the project layout was just created. Callers
// should treat it as a clean "command did not run; please re-invoke"
// signal so a freshly-created store doesn't get a half-finished
// operation written into it. The CLI prints "initialized; please
// re-run your command" on stderr alongside this error.
var ErrNeedsRetry = errors.New("preflight: project just initialized; re-run your command")

// ErrNotInitialized is returned by Ensure when the user declined
// the init prompt. The text matches the legacy hand-rolled message
// so users see exactly what the requirements doc promised:
//
//	j: not initialized; run `j init`
var ErrNotInitialized = errors.New("not initialized; run `j init`")

// UI lets Ensure ask the user whether to run init. The default
// implementation drives a huh.NewConfirm form on the terminal; tests
// inject a scripted fake to avoid touching stdin.
type UI interface {
	// ConfirmInit asks the user whether to run `j init` now. The
	// boolean reports the user's choice (Enter / `y` -> true).
	ConfirmInit(ctx context.Context) (bool, error)
}

// huhUI is the huh-backed implementation of UI. It is unexercised by
// unit tests in headless CI; orchestration is unit-tested through the
// UI interface using a scripted fake.
type huhUI struct {
	in  io.Reader
	out io.Writer
}

// NewHuhUI returns the default huh-backed UI implementation. The
// in / out streams default to the cobra command's stdin / stdout
// when the wiring is applied via PreRunE.
func NewHuhUI(in io.Reader, out io.Writer) UI {
	return &huhUI{in: in, out: out}
}

func (u *huhUI) ConfirmInit(ctx context.Context) (bool, error) {
	v := true
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("j is not initialized in this directory.").
			Description("Run `j init` now?").
			Affirmative("yes").
			Negative("no").
			Value(&v),
	)).WithInput(u.in).WithOutput(u.out).WithTheme(uitheme.Theme()).RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("preflight: %w", err)
	}
	return v, nil
}

// Ensure runs the shared pre-flight check. When ProjectInitialized
// returns true it short-circuits with a nil error and the caller
// proceeds normally. Otherwise it prompts the user via ui:
//
//   - on confirm: store.EnsureProject runs and Ensure surfaces
//     ErrNeedsRetry plus a stderr breadcrumb so the caller exits
//     cleanly and the user re-invokes their command;
//   - on decline: ErrNotInitialized propagates so the CLI exits
//     with the canonical "j: not initialized" message.
//
// stderr is written to only on the accept-path; the decline-path
// relies on the CLI's top-level error printer to render the message.
func Ensure(ctx context.Context, ui UI, stderr io.Writer) error {
	initialized, err := store.ProjectInitialized()
	if err != nil {
		return err
	}
	if initialized {
		return nil
	}
	confirm, err := ui.ConfirmInit(ctx)
	if err != nil {
		return err
	}
	if !confirm {
		return ErrNotInitialized
	}
	if err := store.EnsureProject(); err != nil {
		return err
	}
	fmt.Fprintln(stderr, "initialized; please re-run your command")
	return ErrNeedsRetry
}

// PreRunE is the cobra PersistentPreRunE wired into every subcommand
// that needs an initialized .j layout. It performs config bindings
// (so subcommands inherit the same env-var setup the root command
// does) and then delegates to Ensure with a huh-backed UI. The two
// concerns live in one helper so the cobra layer needs only a single
// reference per subcommand.
func PreRunE(cmd *cobra.Command, _ []string) error {
	if err := config.Init(); err != nil {
		return err
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return Ensure(ctx, NewHuhUI(cmd.InOrStdin(), cmd.OutOrStdout()), cmd.ErrOrStderr())
}
