package work

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"
)

// UI lets the work orchestrator ask the user questions. The default
// implementation renders huh prompts on the terminal; tests substitute
// a scripted fake to avoid touching stdin.
type UI interface {
	AskTarget(ctx context.Context) (string, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
}

// ErrCancelled is returned by the UI when the user aborts a prompt.
var ErrCancelled = errors.New("work: cancelled by user")

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

func (u *huhUI) AskTarget(ctx context.Context) (string, error) {
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
