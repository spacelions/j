package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/cli/taskpick"
	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store"
)

// UI lets `j tasks discard` and `j tasks enter` ask the user
// questions. The default implementation drives huh forms; tests
// substitute a scripted fake to avoid touching stdin. The pattern
// mirrors initcmd.UI / preflight.UI so prompt testability stays
// uniform across the j subcommands.
type UI interface {
	// ConfirmDiscard asks whether to discard task. The boolean reports
	// the user's choice (Enter / `y` -> true). Implementations must
	// translate huh.ErrUserAborted into (false, nil) so a Ctrl-C
	// during the prompt is indistinguishable from an explicit
	// decline.
	ConfirmDiscard(ctx context.Context, task store.Task) (bool, error)
	// PickTask renders a select widget over the supplied tasks and
	// returns the chosen task's id. The bool reports whether a row
	// was actually selected: ok=false collapses both a user-abort
	// (Ctrl-C / Esc) and a defensive empty-input case so callers
	// treat them uniformly as "no selection". The slice is expected
	// to be non-empty and pre-sorted by the caller (the
	// orchestrator already screens the empty-store case before
	// invoking the picker). Behaviour and label format are
	// delegated to internal/cli/taskpick so plan / work / verify /
	// tasks all share one widget.
	PickTask(ctx context.Context, tasks []store.Task) (string, bool, error)
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

func (u *huhUI) ConfirmDiscard(ctx context.Context, task store.Task) (bool, error) {
	v := true
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("Discard task %s?", task.ID)).
			Description(task.Summary).
			Affirmative("yes").
			Negative("no").
			Value(&v),
	)).WithInput(u.in).WithOutput(u.out).WithTheme(uitheme.Theme()).RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("tasks discard: %w", err)
	}
	return v, nil
}

// PickTask delegates to the shared internal/cli/taskpick.Pick widget
// so the label format and abort/empty contract stay uniform across
// every j subcommand. The "Select a task" title is the same the
// previous in-package implementation rendered, so the user-visible
// prompt is unchanged on the `j tasks enter` / `j tasks discard`
// flows.
func (u *huhUI) PickTask(ctx context.Context, tasks []store.Task) (string, bool, error) {
	return taskpick.Pick(ctx, u.in, u.out, "Select a task", tasks)
}

// pickFromStore renders the shared task picker over the rows in s
// and is the single picker entry point used by both `j tasks enter`
// and `j tasks discard` when --id was not supplied. Behaviour:
//
//   - Empty bucket: prints emptyMessage to stdout and returns
//     ("", false, nil); callers short-circuit cleanly with no
//     picker, no confirm, no spawner.
//   - User-abort (Ctrl-C / Esc) or defensive empty input: UI.PickTask
//     returns ok=false and this helper threads it through as
//     ("", false, nil) so callers can recognise the cancel signal
//     via the bool flag.
//   - Happy path: returns (id, true, nil) with id sourced from the
//     scripted UI / huh widget.
//
// Errors from ListTasks or the UI propagate; the UI wraps its own
// errors via the taskpick package so RunDiscard / RunEnter can
// re-emit them without a second wrap.
func pickFromStore(ctx context.Context, s *store.Store, ui UI, stdout io.Writer) (string, bool, error) {
	tasks, err := s.ListTasks()
	if err != nil {
		return "", false, err
	}
	if len(tasks) == 0 {
		fmt.Fprintln(stdout, emptyMessage)
		return "", false, nil
	}
	store.SortTasks(tasks)
	id, ok, err := ui.PickTask(ctx, tasks)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, nil
	}
	return id, true, nil
}
