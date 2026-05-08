// Package picker holds the huh-backed prompts every j subcommand
// composes. Two top-level surfaces:
//
//   - agent picker: SelectTool + SelectModel leaves; PickAgent
//     composite that walks tool → list models → model → CheckLogin;
//     AgentFromStore / StoredInteractive non-UI helpers for the
//     stored-selection path.
//   - source picker: Source enum + SelectSource leaf; PickMarkdownInCwd,
//     PickTask leaves; PickSource composite that drives SelectSource
//     and dispatches to the matching sub-picker.
//
// Plus standalone leaves: ConfirmStatusOverride (yes/no prompt).
// *Picker satisfies the Selector and SourceUI interfaces via duck
// typing so cli commands' narrow UI interfaces drop it in directly;
// tests inject scripted fakes that also satisfy those interfaces.
package picker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/cli/uitheme"
)

// Picker is the huh-backed bundle of leaf prompts used by every j
// subcommand. Construct with New(stdin, stderr); inject the result
// wherever a cli command's narrow UI interface is expected.
type Picker struct {
	in  io.Reader
	out io.Writer
}

// New returns a Picker that drives huh forms against the supplied
// streams. The cobra wirings pass cmd.InOrStdin() / cmd.ErrOrStderr()
// so prompts render on the user's terminal; tests construct Pickers
// against bytes.Buffer for golden-output assertions.
func New(in io.Reader, out io.Writer) *Picker {
	return &Picker{in: in, out: out}
}

// run drives a single huh.Field to completion. A user-abort
// (Ctrl+C / Esc) is surfaced as huh.ErrUserAborted verbatim so each
// cli command's deferred guard recognises the signal via errors.Is
// and exits cleanly with a nil error. Every other error is wrapped
// with a "ui: " prefix so genuine UI failures still surface
// distinguishably.
func (p *Picker) run(ctx context.Context, field huh.Field) error {
	err := huh.NewForm(huh.NewGroup(field)).
		WithInput(p.in).
		WithOutput(p.out).
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

// choose renders a filterable single-select over options and returns
// the chosen value. Empty options surface as a wrapped error mentioning
// the title so callers see immediately which prompt has nothing to
// show; the title is lower-cased for the error message to match the
// pre-extraction wording in plan / work / verify.
func (p *Picker) choose(
	ctx context.Context, title string, options []string,
) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf(
			"%s: no options available", strings.ToLower(title),
		)
	}
	var v string
	if err := p.run(ctx, SelectField(title, options, &v)); err != nil {
		return "", err
	}
	return v, nil
}

// SelectField returns the huh.Select used by every leaf picker
// (PickTask, PickMarkdownInCwd, PickLinearProject, etc.). Exposed so
// teatest-style tests can build the same form the production path
// renders. The caller is responsible for wrapping it in a huh.Form
// (use SelectForm) and for setting Submit/Cancel commands when
// driving it via teatest.NewTestModel.
func SelectField(title string, options []string, value *string) huh.Field {
	return huh.NewSelect[string]().
		Title(title).
		Options(huh.NewOptions(options...)...).
		Filtering(true).
		Value(value)
}

// SelectForm wraps SelectField in a huh.Form themed identically to the
// production picker. teatest tests pass the result to
// teatest.NewTestModel after setting SubmitCmd / CancelCmd.
func SelectForm(title string, options []string, value *string) *huh.Form {
	return huh.NewForm(huh.NewGroup(SelectField(title, options, value))).
		WithTheme(uitheme.Theme())
}
