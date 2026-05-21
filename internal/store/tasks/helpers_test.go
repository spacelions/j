package tasks

import (
	"errors"
	"os"
	"testing"

	"github.com/spacelions/j/internal/store"
)

// crockfordBase32 is the Crockford base32 alphabet used by ULID
// (uppercase, with I/L/O/U excluded). It is duplicated here on
// purpose: the test asserts the observable contract of NewTaskID
// without importing the ULID package, so a regression that swaps in
// a different alphabet still fails.
const crockfordBase32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// openTaskStore chdirs to a fresh temp dir, runs store.EnsureProject, and
// returns a tasks-mode *Store rooted there. Cleanup is registered via
// t.Cleanup so callers do not need to close the store themselves.
func openTaskStore(t *testing.T) *Store {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	dir := DefaultDir()
	s := Open(dir)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// idsOf extracts task IDs preserving slice order. Used by sort tests.
func idsOf(tasks []Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.ID
	}
	return out
}

// equal reports whether two string slices are pairwise identical.
func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// listAllTasks lists every task at the per-cwd tasks dir. Used by
// persist_test.go to assert what PutTask wrote. Returns nil for "no
// tasks dir yet" so the negative-path tests can distinguish "missing"
// from a real read error.
func listAllTasks(t *testing.T) []Task {
	t.Helper()
	dir := DefaultDir()
	if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	s := Open(dir)
	defer func() { _ = s.Close() }()
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	return got
}
