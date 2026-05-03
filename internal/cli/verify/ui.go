package verify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/cli/taskpick"
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
	// tasks (rendered with their summary and id) and returns the
	// chosen task's id. The slice is expected to be non-empty and
	// sorted by the caller (store.SortTasks). The picker now
	// surfaces every task in bbolt regardless of status, with the
	// wrong-status case handled downstream by
	// ConfirmStatusOverride. The bool reports whether a row was
	// actually selected: ok=false collapses both a user-abort
	// (Ctrl-C / Esc) and a defensive empty-input case so callers
	// treat them uniformly as "no selection".
	PickWorkDoneTask(ctx context.Context, tasks []store.Task) (string, bool, error)
	// PickVerifyTask is the picker variant used by `j verify
	// resume`. It shares the underlying widget shape with
	// PickWorkDoneTask but renders a different title so users
	// see immediately which command they are inside. Same
	// (id, ok, err) contract as PickWorkDoneTask.
	PickVerifyTask(ctx context.Context, tasks []store.Task) (string, bool, error)
	// ConfirmStatusOverride asks the user to confirm proceeding
	// when the resolved task's status falls outside the natural
	// `j verify` allowlist. cmd is the command label rendered
	// into the prompt ("verify"); taskID and status come from
	// the row. A `false` return means the user declined; the
	// orchestrator surfaces a clean nil error in that case.
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
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

// PickWorkDoneTask delegates to the shared internal/cli/taskpick.Pick
// widget so the label format ("<id> — <status> — <summary>") and
// the abort/empty contract stay uniform across every j subcommand.
func (u *huhUI) PickWorkDoneTask(ctx context.Context, tasks []store.Task) (string, bool, error) {
	return taskpick.Pick(ctx, u.in, u.out, "Select a task to verify", tasks)
}

// PickVerifyTask is the resume-flow variant: same widget body as
// PickWorkDoneTask, different title so users can tell at a glance
// which command they are inside.
func (u *huhUI) PickVerifyTask(ctx context.Context, tasks []store.Task) (string, bool, error) {
	return taskpick.Pick(ctx, u.in, u.out, "Select a task to resume verifying", tasks)
}

// ConfirmStatusOverride renders a yes/no prompt when the resolved
// task's status falls outside the verify allowlist. The default
// answer is "no" so a stray Enter does not run agent.Verify
// against a task that's still in flight or already past the
// verify phase. huh.ErrUserAborted is propagated verbatim and the
// caller's deferred guard converts it to a nil return.
func (u *huhUI) ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error) {
	title := fmt.Sprintf("Task %s is in status %s; %s anyway?", taskID, status, cmd)
	v := false
	if err := u.run(ctx, huh.NewConfirm().
		Title(title).
		Affirmative("yes").
		Negative("no").
		Value(&v)); err != nil {
		return false, err
	}
	return v, nil
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
