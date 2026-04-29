package plan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/store"
)

// UI lets the planner ask the user questions. The default implementation
// renders huh prompts on the terminal; tests substitute a scripted fake
// to avoid touching stdin.
type UI interface {
	// SelectSource asks the user which planning source to use. It is
	// only invoked when no markdown source was supplied via
	// --from-file or PLAN_FROM_FILE.
	SelectSource(ctx context.Context) (PlanSource, error)
	// AskFromFile prompts the user for the markdown source path. It is
	// invoked after SelectSource returns SourceMarkdown.
	AskFromFile(ctx context.Context) (string, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
	// PickPlanTask asks the user to choose one of the supplied tasks
	// (rendered with their summary and id) and returns the chosen
	// task's id. The slice is expected to be non-empty and sorted by
	// the caller. Used by `j plan resume` to pick a session to
	// resume; mirrors the same-named helper in `internal/cli/work`.
	PickPlanTask(ctx context.Context, tasks []store.Task) (string, error)
}

// ErrCancelled is returned by the UI when the user aborts a prompt.
var ErrCancelled = errors.New("plan: cancelled by user")

// huhUI is the huh-backed implementation of UI. The methods drive real
// huh.Form instances and so are not exercised by unit tests in headless
// CI; orchestration logic is unit-tested through the UI interface using
// a scripted fake (see plan_test.go).
type huhUI struct {
	in  io.Reader
	out io.Writer
}

func newHuhUI(in io.Reader, out io.Writer) *huhUI {
	return &huhUI{in: in, out: out}
}

func (u *huhUI) SelectSource(ctx context.Context) (PlanSource, error) {
	label, err := u.choose(ctx, "Select plan source", SourceLabels)
	if err != nil {
		return 0, err
	}
	return ParseSource(label)
}

func (u *huhUI) AskFromFile(ctx context.Context) (string, error) {
	var v string
	if err := u.run(ctx, huh.NewInput().
		Title("Markdown file location").
		Placeholder("/path/to/feature.md").
		Value(&v)); err != nil {
		return "", err
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", errors.New("plan: no markdown file location provided")
	}
	return v, nil
}

func (u *huhUI) SelectTool(ctx context.Context, options []string) (string, error) {
	return u.choose(ctx, "Select planning tool", options)
}

func (u *huhUI) SelectModel(ctx context.Context, options []string) (string, error) {
	return u.choose(ctx, "Select model", options)
}

// PickPlanTask renders the resume picker for `j plan resume`. The
// label format mirrors `internal/cli/work`'s identical helper so
// users see the same shape (`<id> — <summary>`) across the two
// resume flows. The huh widget is not exercised in headless CI;
// orchestration logic in resume.go is unit-tested through the UI
// interface using a scripted fake.
func (u *huhUI) PickPlanTask(ctx context.Context, tasks []store.Task) (string, error) {
	if len(tasks) == 0 {
		return "", errors.New("pick plan task: no plan sessions available")
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
	chosen, err := u.choose(ctx, "Select a plan session to resume", labels)
	if err != nil {
		return "", err
	}
	id, ok := byLabel[chosen]
	if !ok {
		return "", fmt.Errorf("pick plan task: unknown selection %q", chosen)
	}
	return id, nil
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
		Height(12).
		Value(&v)); err != nil {
		return "", err
	}
	return v, nil
}

func (u *huhUI) run(ctx context.Context, field huh.Field) error {
	err := huh.NewForm(huh.NewGroup(field)).
		WithInput(u.in).
		WithOutput(u.out).
		RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrCancelled
	}
	if err != nil {
		return fmt.Errorf("ui: %w", err)
	}
	return nil
}
