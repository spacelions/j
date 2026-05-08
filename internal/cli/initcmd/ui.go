package initcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/cli/uitheme"
)

// UI lets `j init` ask the user whether to wipe an existing layout.
// The default implementation drives a huh.NewConfirm form; tests
// substitute a scripted fake to avoid touching stdin.
type UI interface {
	// ConfirmReset asks whether to remove `.j/` and recreate it. The
	// boolean reports the user's choice (Enter / `y` -> true).
	ConfirmReset(ctx context.Context) (bool, error)
}

// huhUI is the huh-backed UI implementation. The form is driven on
// the terminal and so is not exercised by unit tests in headless CI;
// orchestration is unit-tested through the UI interface using a
// scripted fake.
type huhUI struct {
	in  io.Reader
	out io.Writer
}

func newHuhUI(in io.Reader, out io.Writer) *huhUI {
	return &huhUI{in: in, out: out}
}

func (u *huhUI) ConfirmReset(ctx context.Context) (bool, error) {
	v := true
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Reset .j/ in this directory?").
			Description("This removes any existing j state.").
			Affirmative("yes").
			Negative("no").
			Value(&v),
	)).WithInput(u.in).WithOutput(u.out).
		WithTheme(uitheme.Theme()).RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("init: %w", err)
	}
	return v, nil
}

// Accept reports whether s is treated as "yes" in the init prompt:
// empty (Enter), `y`, or `yes`, case-insensitive.
func Accept(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "y", "yes":
		return true
	}
	return false
}
