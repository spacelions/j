package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store"
)

// UI lets `j tasks delete` and `j tasks enter` ask the user
// questions. The default implementation drives huh forms; tests
// substitute a scripted fake to avoid touching stdin. The pattern
// mirrors initcmd.UI / preflight.UI so prompt testability stays
// uniform across the j subcommands.
type UI interface {
	// ConfirmDelete asks whether to delete task. The boolean reports
	// the user's choice (Enter / `y` -> true). Implementations must
	// translate huh.ErrUserAborted into (false, nil) so a Ctrl-C
	// during the prompt is indistinguishable from an explicit
	// decline.
	ConfirmDelete(ctx context.Context, task store.Task) (bool, error)
	// PickTask renders a select widget over the supplied tasks and
	// returns the chosen task's id. Implementations must translate
	// huh.ErrUserAborted into ("", nil) so a Ctrl-C / Esc is
	// indistinguishable from an explicit cancel and callers can
	// treat an empty id as the cancel signal. The slice is expected
	// to be non-empty and pre-sorted by the caller (the orchestrator
	// already screens the empty-store case before invoking the
	// picker).
	PickTask(ctx context.Context, tasks []store.Task) (string, error)
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
	)).WithInput(u.in).WithOutput(u.out).WithTheme(uitheme.Theme()).RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("tasks delete: %w", err)
	}
	return v, nil
}

// PickTask renders a single huh.NewSelect[string] over the supplied
// tasks. The label shape (`<id> — <status> — <summary>`) matches the
// requirements doc and is built by the package-private helper
// formatEnterLabels so the unit test can pin the format without
// driving a real huh widget. A user-abort (Ctrl-C / Esc) collapses
// to ("", nil) so the orchestrator can treat an empty id as a clean
// cancel — same convention as ConfirmDelete.
func (u *huhUI) PickTask(ctx context.Context, tasks []store.Task) (string, error) {
	labels, byLabel := formatEnterLabels(tasks)
	var chosen string
	err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Select a task").
			Options(huh.NewOptions(labels...)...).
			Filtering(true).
			Height(12).
			Value(&chosen),
	)).WithInput(u.in).WithOutput(u.out).WithTheme(uitheme.Theme()).RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("tasks ui: %w", err)
	}
	id, ok := byLabel[chosen]
	if !ok {
		return "", fmt.Errorf("tasks ui: unknown selection %q", chosen)
	}
	return id, nil
}

// formatEnterLabels builds the `<id> — <status> — <summary>` label
// list and a reverse lookup from label to task id. An empty Summary
// falls back to "(no summary)" so every row stays selectable. The
// helper is package-private so the unit test pins the label shape
// without driving a real huh widget.
func formatEnterLabels(tasks []store.Task) (labels []string, byLabel map[string]string) {
	labels = make([]string, 0, len(tasks))
	byLabel = make(map[string]string, len(tasks))
	for _, t := range tasks {
		summary := strings.TrimSpace(t.Summary)
		if summary == "" {
			summary = "(no summary)"
		}
		label := fmt.Sprintf("%s — %s — %s", t.ID, t.Status, summary)
		labels = append(labels, label)
		byLabel[label] = t.ID
	}
	return labels, byLabel
}

// pickFromStore renders the shared task picker over the rows in s
// and is the single picker entry point used by both `j tasks enter`
// and `j tasks delete` when --id was not supplied. Behaviour:
//
//   - Empty bucket: prints emptyMessage to stdout and returns
//     ("", false, nil); callers short-circuit cleanly with no
//     picker, no confirm, no spawner.
//   - User-abort (Ctrl-C / Esc): UI.PickTask returns ("", nil) and
//     this helper threads it through as ("", false, nil) so callers
//     can recognise the cancel signal via the bool flag.
//   - Happy path: returns (id, true, nil) with id sourced from the
//     scripted UI / huh widget.
//
// Errors from ListTasks or the UI propagate; the UI wraps its own
// errors with the "tasks ui:" prefix so RunDelete / RunEnter can
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
	id, err := ui.PickTask(ctx, tasks)
	if err != nil {
		return "", false, err
	}
	if id == "" {
		return "", false, nil
	}
	return id, true, nil
}
