package resolver

import (
	"errors"

	"github.com/charmbracelet/huh"
)

// CleanAbort converts a huh.ErrUserAborted from any prompt below into
// a nil return so an explicit Ctrl-C / Esc exits the command cleanly
// without surfacing as a "cancelled by user" error. Apply via deferred
// function in plan.Run / work.Run / verify.Run:
//
//	func Run(ctx context.Context, opts Options) (err error) {
//	    defer func() { err = resolver.CleanAbort(err) }()
//	    ...
//	}
//
// Genuine UI errors keep their wrapping.
func CleanAbort(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		return nil
	}
	return err
}
