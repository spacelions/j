package resolver

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/store"
)

func TestCleanAbort(t *testing.T) {
	if got := CleanAbort(huh.ErrUserAborted); got != nil {
		t.Fatalf("CleanAbort abort = %v, want nil", got)
	}
	err := errors.New("boom")
	if got := CleanAbort(err); !errors.Is(got, err) {
		t.Fatalf("CleanAbort boom = %v", got)
	}
}

func TestParseMustRead(t *testing.T) {
	got := ParseMustRead(" AGENTS.md ; plan.md;; notes.md ")
	want := []string{"AGENTS.md", "plan.md", "notes.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseMustRead = %#v, want %#v", got, want)
	}
	if got := ParseMustRead(" ; "); got != nil {
		t.Fatalf("empty ParseMustRead = %#v", got)
	}
}

func TestMustRead(t *testing.T) {
	setupResolverProject(t)
	got, err := MustRead()
	if err != nil {
		t.Fatalf("MustRead empty: %v", err)
	}
	if got != nil {
		t.Fatalf("empty MustRead = %#v", got)
	}
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket(store.BucketProject); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketProject, KeyMustRead, "AGENTS.md; plan.md"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	got, err = MustRead()
	if err != nil {
		t.Fatalf("MustRead stored: %v", err)
	}
	want := []string{"AGENTS.md", "plan.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MustRead = %#v, want %#v", got, want)
	}
}

func TestMustReadOpenError(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(filepath.Join(".j"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MustRead(); err == nil {
		t.Fatal("MustRead error = nil")
	}
}
