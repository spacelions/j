package linear

import (
	"fmt"
	"regexp"
)

// identifierPattern is Linear's `<TEAM>-<NUM>` shape: an upper-case
// team prefix (one letter followed by one or more upper-case letters
// or digits) joined to a numeric counter via `-`. The pattern is
// case-sensitive so `eng-123` rejects with ErrInvalidIdentifier; the
// cli relies on that to keep the bbolt identifier round-trip stable.
var identifierPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]+-[0-9]+$`)

// ValidateIdentifier returns nil iff id matches identifierPattern.
// A non-nil error wraps ErrInvalidIdentifier with the offending value
// so callers can use errors.Is and surface the original string.
func ValidateIdentifier(id string) error {
	if !identifierPattern.MatchString(id) {
		return fmt.Errorf("%w: %q", ErrInvalidIdentifier, id)
	}
	return nil
}
