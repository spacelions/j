// Package picker holds the leaf huh-backed prompt widgets that every
// j subcommand composes. The package never owns flow control: cli
// command bodies still drive their own switch / case logic after a
// leaf returns. Examples:
//
//   - `j plan` and `j tasks start` call SelectSource (markdown |
//     linear | task) and dispatch to PickMarkdownInCwd / PickTask
//     themselves.
//   - `j work` and `j verify` skip SelectSource entirely and call
//     PickTask directly with their own title.
//
// Picker satisfies internal/cli/agentpick.Selector via SelectTool /
// SelectModel, so cobra wirings can drop it into any caller that
// expected the existing interface.
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
func (p *Picker) choose(ctx context.Context, title string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("%s: no options available", strings.ToLower(title))
	}
	var v string
	if err := p.run(ctx, huh.NewSelect[string]().
		Title(title).
		Options(huh.NewOptions(options...)...).
		Filtering(true).
		Value(&v)); err != nil {
		return "", err
	}
	return v, nil
}
