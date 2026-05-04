// Package testutil holds shared test helpers used across the j
// codebase.
//
// CLI tests that drive cobra.Execute hit the shared preflight check,
// which asks for project.must_read on first run. Init lays down the .j
// layout AND seeds an empty must_read value so the preflight
// short-circuits without driving the huh prompt (which would otherwise
// hang on stdin in a headless test).
package testutil

import (
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
)

type testTB interface {
	Helper()
	Fatalf(format string, args ...any)
}

// Init lays down the .j layout in the current working directory and
// seeds project.must_read="" so the cobra preflight short-circuits.
// Tests must call this helper after t.Chdir.
func Init(t testTB) {
	t.Helper()
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("testutil: EnsureProject: %v", err)
	}
	SeedMustRead(t)
}

// SeedMustRead persists an empty project.must_read value so the
// preflight check short-circuits without driving the huh prompt.
// Use it directly when EnsureProject has already run.
func SeedMustRead(t testTB) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("testutil: DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("testutil: Open: %v", err)
	}
	if err := s.Put(store.BucketProject, resolver.KeyMustRead, ""); err != nil {
		_ = s.Close()
		t.Fatalf("testutil: Put must_read: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("testutil: Close: %v", err)
	}
}
