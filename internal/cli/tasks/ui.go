package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/store"
)

// UI lets `j tasks delete` ask the user to confirm a destructive
// removal. The default implementation drives a huh.NewConfirm form;
// tests substitute a scripted fake to avoid touching stdin. The
// pattern mirrors initcmd.UI / preflight.UI so prompt testability
// stays uniform across the j subcommands.
type UI interface {
	// ConfirmDelete asks whether to delete task. The boolean reports
	// the user's choice (Enter / `y` -> true). Implementations must
	// translate huh.ErrUserAborted into (false, nil) so a Ctrl-C
	// during the prompt is indistinguishable from an explicit
	// decline.
	ConfirmDelete(ctx context.Context, task store.Task) (bool, error)
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

func (u *huhUI) ConfirmDelete(ctx context.Context, task store.Task) (bool, error) {
	v := true
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("Delete task %s?", task.ID)).
			Description(task.Summary).
			Affirmative("yes").
			Negative("no").
			Value(&v),
	)).WithInput(u.in).WithOutput(u.out).RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("tasks delete: %w", err)
	}
	return v, nil
}
