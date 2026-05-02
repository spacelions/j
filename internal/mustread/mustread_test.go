package mustread

import (
	"reflect"
	"testing"

	"github.com/spacelions/j/internal/store"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   \t  ", nil},
		{"single", "AGENTS.md", []string{"AGENTS.md"}},
		{"multiple", "AGENTS.md;CLAUDE.md", []string{"AGENTS.md", "CLAUDE.md"}},
		{"trims", " AGENTS.md ; CLAUDE.md ", []string{"AGENTS.md", "CLAUDE.md"}},
		{"drops empty fragments", "AGENTS.md;;CLAUDE.md;", []string{"AGENTS.md", "CLAUDE.md"}},
		{"preserves case", "AgEnTs.MD;clAude.md", []string{"AgEnTs.MD", "clAude.md"}},
		{"only separators", ";;;", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Parse(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Parse(%q) = %#v, want %#v", c.in, got, c.want)
			}
		})
	}
}

func TestLoad_RoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if _, set, err := Load(s); err != nil || set {
		t.Fatalf("Load on fresh DB: set=%v err=%v", set, err)
	}
	if err := s.Put(store.BucketProject, Key, "AGENTS.md;CLAUDE.md"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, set, err := Load(s)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !set {
		t.Fatal("Load: set=false after Put")
	}
	if got != "AGENTS.md;CLAUDE.md" {
		t.Fatalf("Load = %q, want preserved case", got)
	}

	// Empty string round-trip: still reported as set.
	if err := s.Put(store.BucketProject, Key, ""); err != nil {
		t.Fatalf("Put empty: %v", err)
	}
	got, set, err = Load(s)
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	if !set || got != "" {
		t.Fatalf("Load empty = (%q, %v), want (\"\", true)", got, set)
	}
}

// TestLoad_PathStability confirms Key/bucket constants don't drift.
func TestLoad_PathStability(t *testing.T) {
	if store.BucketProject != "project" {
		t.Fatalf("BucketProject = %q, want project", store.BucketProject)
	}
	if Key != "mustread" {
		t.Fatalf("Key = %q, want mustread", Key)
	}
}
