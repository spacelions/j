package work

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

// UI lets the work orchestrator ask the user questions. The default
// implementation renders huh prompts on the terminal; tests substitute
// a scripted fake to avoid touching stdin.
type UI interface {
	// AskFromFile prompts the user for a legacy plan markdown path.
	// Used only when --from-file/-f / WORK_FROM_FILE is empty AND no
	// `plan-done` task is available in bbolt; the resolution helper
	// in work.go calls it as the last fallback before erroring out.
	AskFromFile(ctx context.Context) (string, error)
	// PickPlanTask asks the user to choose one of the supplied tasks
	// (rendered with their summary and id) and returns the chosen
	// task's id. The slice is expected to be non-empty and sorted by
	// the caller (most-recent plan-done first). Used by the
	// non-resume `j work` flow.
	PickPlanTask(ctx context.Context, tasks []store.Task) (string, error)
	// PickWorkTask is the picker variant used by `j work resume`. It
	// shares the underlying widget shape with PickPlanTask but
	// renders a different title (`Select a task to resume`)
	// so users see immediately which command they are inside.
	PickWorkTask(ctx context.Context, tasks []store.Task) (string, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
}

// huhUI is the huh-backed implementation of UI. The methods drive real
// huh.Form instances and so are not exercised by unit tests in headless
// CI; orchestration logic is unit-tested through the UI interface using
// a scripted fake (see work_test.go).
type huhUI struct {
	in  io.Reader
	out io.Writer
}

func newHuhUI(in io.Reader, out io.Writer) *huhUI {
	return &huhUI{in: in, out: out}
}

func (u *huhUI) AskFromFile(ctx context.Context) (string, error) {
	var v string
	if err := u.run(ctx, huh.NewInput().
		Title("Plan markdown file location").
		Placeholder("/path/to/feature.plan.md").
		Value(&v)); err != nil {
		return "", err
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", errors.New("work: no plan markdown file location provided")
	}
	return v, nil
}

func (u *huhUI) PickPlanTask(ctx context.Context, tasks []store.Task) (string, error) {
	return u.pickTask(ctx, "Select a plan-done task", "pick plan task", tasks)
}

// PickWorkTask is the resume-flow variant: it shares the shape of
// PickPlanTask but uses a different title and error prefix so users
// can tell at a glance which command they are inside.
func (u *huhUI) PickWorkTask(ctx context.Context, tasks []store.Task) (string, error) {
	return u.pickTask(ctx, "Select a task to resume", "pick work task", tasks)
}

// pickTask is the shared widget body behind PickPlanTask /
// PickWorkTask. The title string is the only user-visible
// difference; errPrefix tags the error messages so logs from the
// two flows are distinguishable.
func (u *huhUI) pickTask(ctx context.Context, title, errPrefix string, tasks []store.Task) (string, error) {
	if len(tasks) == 0 {
		return "", fmt.Errorf("%s: no tasks available", errPrefix)
	}
	labels := make([]string, 0, len(tasks))
	byLabel := make(map[string]string, len(tasks))
	for _, t := range tasks {
		summary := strings.TrimSpace(t.Summary)
		if summary == "" {
			summary = "(no summary)"
		}
		label := fmt.Sprintf("%s — %s", t.ID, summary)
		labels = append(labels, label)
		byLabel[label] = t.ID
	}
	chosen, err := u.choose(ctx, title, labels)
	if err != nil {
		return "", err
	}
	id, ok := byLabel[chosen]
	if !ok {
		return "", fmt.Errorf("%s: unknown selection %q", errPrefix, chosen)
	}
	return id, nil
}

func (u *huhUI) SelectTool(ctx context.Context, options []string) (string, error) {
	return u.choose(ctx, "Select coding tool", options)
}

func (u *huhUI) SelectModel(ctx context.Context, options []string) (string, error) {
	return u.choose(ctx, "Select model", options)
}

func (u *huhUI) choose(ctx context.Context, title string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("%s: no options available", strings.ToLower(title))
	}
	var v string
	if err := u.run(ctx, huh.NewSelect[string]().
		Title(title).
		Options(huh.NewOptions(options...)...).
		Filtering(true).
		Value(&v)); err != nil {
		return "", err
	}
	return v, nil
}

// run drives a single huh.Field to completion. A user-abort
// (Ctrl+C / Esc) is surfaced as huh.ErrUserAborted verbatim so the
// orchestrator's Run / RunResume can recognise the signal via
// errors.Is and exit cleanly with a nil error. Every other error is
// wrapped with a "ui: " prefix so genuine UI failures still surface
// distinguishably.
func (u *huhUI) run(ctx context.Context, field huh.Field) error {
	err := huh.NewForm(huh.NewGroup(field)).
		WithInput(u.in).
		WithOutput(u.out).
		WithTheme(uitheme.Theme()).
		RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return huh.ErrUserAborted
	}
	if err != nil {
		return fmt.Errorf("ui: %w", err)
	}
	return nil
}
