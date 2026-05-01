package verify

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

// errEmptyFromFile is returned by AskFromFile when the user submits
// an empty / whitespace-only path. The literal is package-private so
// unit tests can pin its message via Error() without introducing a
// runtime seam.
var errEmptyFromFile = errors.New("J: no markdown provided")

// UI lets the verify orchestrator ask the user questions. The
// default implementation renders huh prompts on the terminal; tests
// substitute a scripted fake to avoid touching stdin. The shape
// mirrors `work.UI` exactly so the agentpick.Selector slice
// (SelectTool / SelectModel) stays satisfied without an adapter.
type UI interface {
	// AskFromFile prompts the user for a legacy plan markdown
	// path. Used only for symmetry with the work UI; `j verify`
	// never reads from a free-form file today, but keeping the
	// method makes the UI shape interchangeable so tests can
	// exercise both flows with one scripted fake.
	AskFromFile(ctx context.Context) (string, error)
	// PickWorkDoneTask asks the user to pick one of the supplied
	// work-done / verify-done / help tasks (rendered with their
	// summary and id) and returns the chosen task's id. The slice
	// is expected to be non-empty and sorted by the caller.
	PickWorkDoneTask(ctx context.Context, tasks []store.Task) (string, error)
	// PickVerifyTask is the picker variant used by `j verify
	// resume`. It shares the underlying widget shape with
	// PickWorkDoneTask but renders a different title so users
	// see immediately which command they are inside.
	PickVerifyTask(ctx context.Context, tasks []store.Task) (string, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
}

// huhUI is the huh-backed implementation of UI. The methods drive
// real huh.Form instances and are not exercised by unit tests in
// headless CI; orchestration logic is unit-tested through the UI
// interface using a scripted fake (see verify_test.go). Mirrors
// work.huhUI with two title swaps for the picker variants.
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
		return "", errEmptyFromFile
	}
	return v, nil
}

func (u *huhUI) PickWorkDoneTask(ctx context.Context, tasks []store.Task) (string, error) {
	return u.pickTask(ctx, "Select a work-done task", "pick verify task", tasks)
}

func (u *huhUI) PickVerifyTask(ctx context.Context, tasks []store.Task) (string, error) {
	return u.pickTask(ctx, "Select a task to resume verifying", "pick verify resume task", tasks)
}

// pickTask is the shared widget body behind PickWorkDoneTask /
// PickVerifyTask. The title string is the only user-visible
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
	return u.choose(ctx, "Select verifier tool", options)
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
