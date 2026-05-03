package plan

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

// UI lets the planner ask the user questions. The default implementation
// renders huh prompts on the terminal; tests substitute a scripted fake
// to avoid touching stdin.
type UI interface {
	// SelectSource asks the user which planning source to use. It is
	// only invoked when no markdown source was supplied via
	// --from-file or PLAN_FROM_FILE.
	SelectSource(ctx context.Context) (PlanSource, error)
	// PickFromFile asks the user to choose one of the markdown files
	// the orchestrator scanned out of the cwd. The slice is the
	// pre-sorted, pre-filtered basename list (AGENTS.md / README.md
	// / hidden files already removed); the chosen basename is
	// returned verbatim so the orchestrator can map it back to the
	// matching absolute path. Invoked only after SelectSource
	// returns SourceMarkdown.
	PickFromFile(ctx context.Context, options []string) (string, error)
	SelectTool(ctx context.Context, options []string) (string, error)
	SelectModel(ctx context.Context, options []string) (string, error)
	// PickPlanTask asks the user to choose one of the supplied tasks
	// (rendered with their summary and id) and returns the chosen
	// task's id. The slice is expected to be non-empty and sorted by
	// the caller. Used by `j plan resume` to pick a session to
	// resume; mirrors the same-named helper in `internal/cli/work`.
	// The bool reports whether a row was actually selected: ok=false
	// collapses both a user-abort (Ctrl-C / Esc) and a defensive
	// empty-input case so callers treat them uniformly as "no
	// selection".
	PickPlanTask(ctx context.Context, tasks []store.Task) (string, bool, error)
	// PickReplanTask is the picker variant used by the new
	// "re-plan an existing task" SelectSource entry and by the
	// no-flag `j plan` picker reachable from `--from-task`. It
	// shares the underlying widget shape with PickPlanTask but
	// renders a different title so users see immediately which
	// flow they are inside. The slice is expected to be non-empty
	// and sorted by the caller (store.SortTasks); the bool follows
	// the same ok=false-means-no-selection contract as
	// PickPlanTask.
	PickReplanTask(ctx context.Context, tasks []store.Task) (string, bool, error)
	// ConfirmStatusOverride asks the user to confirm proceeding
	// when the resolved task's status falls outside the natural
	// allowlist for the current command. cmd is the command label
	// rendered into the prompt (e.g. "re-plan", "work", "verify");
	// taskID and status come from the resolved task row. A `false`
	// return means the user declined; the orchestrator should
	// surface a clean nil error in that case (consistent with
	// huh.ErrUserAborted handling).
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
}

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

// PickFromFile renders the second-level markdown picker. Empty-options
// validation, filtering / scrolling height, and user-abort propagation
// are inherited verbatim from the shared `choose` helper so the picker
// behaves identically to the tool/model/resume selectors. The
// orchestrator is responsible for filling `options` with the
// pre-sorted, pre-filtered basenames produced by mdfile.ListInDir.
func (u *huhUI) PickFromFile(ctx context.Context, options []string) (string, error) {
	return u.choose(ctx, "Select markdown file", options)
}

func (u *huhUI) SelectTool(ctx context.Context, options []string) (string, error) {
	return u.choose(ctx, "Select planning tool", options)
}

func (u *huhUI) SelectModel(ctx context.Context, options []string) (string, error) {
	return u.choose(ctx, "Select model", options)
}

// PickPlanTask renders the resume picker for `j plan resume`. The
// label format ("<id> — <status> — <summary>") is delegated to the
// shared internal/cli/taskpick package so the four j subcommands
// agree on a single picker shape.
func (u *huhUI) PickPlanTask(ctx context.Context, tasks []store.Task) (string, bool, error) {
	return taskpick.Pick(ctx, u.in, u.out, "Select a plan session to resume", tasks)
}

// PickReplanTask renders the picker for the re-plan flow. The
// title differs from PickPlanTask so users can tell at a glance
// which command they are inside (resume vs re-plan); the widget
// body is shared with the other j pickers via taskpick.Pick.
func (u *huhUI) PickReplanTask(ctx context.Context, tasks []store.Task) (string, bool, error) {
	return taskpick.Pick(ctx, u.in, u.out, "Select a task to re-plan", tasks)
}

// ConfirmStatusOverride renders a yes/no prompt when a resolved
// task's status falls outside the command's allowlist. The default
// answer is "no" so a stray Enter does not run agent.Plan against
// a task that's still in flight. huh.ErrUserAborted from this
// prompt is propagated verbatim and the caller's deferred guard
// converts it to a nil return.
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
