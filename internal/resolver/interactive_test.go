package resolver

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/store"
)

func openTestSettings(t *testing.T, bucket string) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.EnsureBucket(bucket); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func boolPtr(b bool) *bool { return &b }

func TestInteractive_ExplicitWinsOverStored(t *testing.T) {
	s := openTestSettings(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "interactive", "false"); err != nil {
		t.Fatal(err)
	}
	if got := Interactive(s, io.Discard, store.BucketPlanner, boolPtr(true)); got != true {
		t.Fatalf("Interactive = %v, want true (explicit wins)", got)
	}
}

func TestInteractive_StoredFalseOverridesDefault(t *testing.T) {
	s := openTestSettings(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "interactive", "false"); err != nil {
		t.Fatal(err)
	}
	if got := Interactive(s, io.Discard, store.BucketPlanner, nil); got != false {
		t.Fatalf("Interactive = %v, want false (stored)", got)
	}
}

func TestInteractive_StoredTrueOverridesDefault(t *testing.T) {
	s := openTestSettings(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "interactive", "true"); err != nil {
		t.Fatal(err)
	}
	if got := Interactive(s, io.Discard, store.BucketPlanner, nil); got != true {
		t.Fatalf("Interactive = %v, want true (stored)", got)
	}
}

func TestInteractive_StoredUnparseableFallsToDefault(t *testing.T) {
	s := openTestSettings(t, store.BucketPlanner)
	if err := s.Put(store.BucketPlanner, "interactive", "garbage"); err != nil {
		t.Fatal(err)
	}
	if got := Interactive(s, io.Discard, store.BucketPlanner, nil); got != true {
		t.Fatalf("Interactive = %v, want true (default)", got)
	}
}

func TestInteractive_MissingKeyFallsToDefault(t *testing.T) {
	s := openTestSettings(t, store.BucketPlanner)
	if got := Interactive(s, io.Discard, store.BucketPlanner, nil); got != true {
		t.Fatalf("Interactive = %v, want true (default)", got)
	}
}

func TestInteractive_NilStoreFallsToDefault(t *testing.T) {
	if got := Interactive(nil, io.Discard, store.BucketPlanner, nil); got != true {
		t.Fatalf("Interactive = %v, want true (default)", got)
	}
}

func TestInteractive_NilStoreExplicitWins(t *testing.T) {
	if got := Interactive(nil, io.Discard, store.BucketPlanner, boolPtr(false)); got != false {
		t.Fatalf("Interactive = %v, want false (explicit)", got)
	}
}

func TestInteractive_NilStoreOpensDefault(t *testing.T) {
	setupResolverProject(t)
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket(store.BucketPlanner); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketPlanner, "interactive", "false"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := Interactive(nil, io.Discard, store.BucketPlanner, nil); got {
		t.Fatal("Interactive = true, want stored false")
	}
}
