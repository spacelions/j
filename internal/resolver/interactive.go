package resolver

import (
	"io"
	"strconv"

	"github.com/spacelions/j/internal/store"
)

// Interactive returns the effective interactive flag for the given
// bucket. Precedence: explicit (non-nil pointer) > stored bucket value
// (parseable bool) > default true.
//
// When opts.Store is nil the helper opens `<cwd>/.j/settings` for the
// duration of the read and releases the lock immediately so the file
// lock is never held across the agent invocation.
func Interactive(s *store.Store, stderr io.Writer, bucket string, explicit *bool) bool {
	if explicit != nil {
		return *explicit
	}
	if v, ok := storedInteractive(s, stderr, bucket); ok {
		return v
	}
	return true
}

func storedInteractive(s *store.Store, stderr io.Writer, bucket string) (bool, bool) {
	if s != nil {
		return readInteractive(s, bucket)
	}
	opened, ok := store.OpenSettings(stderr)
	if !ok {
		return false, false
	}
	defer func() { _ = opened.Close() }()
	return readInteractive(opened, bucket)
}

func readInteractive(s *store.Store, bucket string) (bool, bool) {
	v, ok, err := s.Get(bucket, "interactive")
	if err != nil || !ok || v == "" {
		return false, false
	}
	parsed, perr := strconv.ParseBool(v)
	if perr != nil {
		return false, false
	}
	return parsed, true
}
