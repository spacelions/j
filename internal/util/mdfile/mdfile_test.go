package mdfile

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestResolve_Markdown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, ext := range []string{".md", ".markdown"} {
		t.Run(ext, func(t *testing.T) {
			p := filepath.Join(dir, "spec"+ext)
			if err := os.WriteFile(p, []byte("hi"), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := Resolve(p)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			want, err := filepath.Abs(p)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestResolve_TrimsAndUppercaseExtension(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "SPEC.MD")
	if err := os.WriteFile(p, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve("  " + p + "  ")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want, _ := filepath.Abs(p)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolve_Errors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	txt := filepath.Join(dir, "spec.txt")
	if err := os.WriteFile(txt, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		in      string
		wantSub string
	}{
		{"empty", "", "empty"},
		{"missing", filepath.Join(dir, "nope.md"), "stat"},
		{"directory", dir, "directory"},
		{"non-markdown", txt, "not a markdown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Resolve(tc.in)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func TestResolve_NotRegularFile(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("posix-only fifo test")
	}
	dir := t.TempDir()
	pipe := filepath.Join(dir, "spec.md")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(pipe) })

	_, err := Resolve(pipe)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("err = %v", err)
	}
}

// listInDirBasenames is a tiny helper that runs ListInDir and then
// strips the directory prefix off each absolute path so test
// expectations stay readable. Relative-path expectations would defeat
// the absolute-path contract.
func listInDirBasenames(t *testing.T, dir string) []string {
	t.Helper()
	got, err := ListInDir(dir)
	if err != nil {
		t.Fatalf("ListInDir: %v", err)
	}
	out := make([]string, len(got))
	for i, p := range got {
		if !filepath.IsAbs(p) {
			t.Fatalf("ListInDir returned non-absolute path %q", p)
		}
		out[i] = filepath.Base(p)
	}
	return out
}

// TestListInDir_FiltersAndSorts exercises the happy path: mixed
// extensions are kept (case-insensitively), `.txt` and the fixed
// AGENTS.md / README.md exclusions drop out, hidden files are
// skipped, and the result is sorted case-insensitively by basename.
func TestListInDir_FiltersAndSorts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := []string{
		"spec.md",
		"NOTES.markdown",
		"alpha.MD",
		"BETA.Markdown",
		"draft.txt",
		"AGENTS.md",
		"agents.MD",
		"README.md",
		"readme.MD",
		".draft.md",
		".env",
	}
	for _, name := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subdir", "nested.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := listInDirBasenames(t, dir)
	want := []string{"alpha.MD", "BETA.Markdown", "NOTES.markdown", "spec.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListInDir basenames = %v, want %v", got, want)
	}
}

// TestListInDir_EmptyDirectory pins the empty-but-readable
// contract: ListInDir returns an empty (non-nil error) result so
// callers can branch on len(out) without inspecting err.
func TestListInDir_EmptyDirectory(t *testing.T) {
	t.Parallel()
	got, err := ListInDir(t.TempDir())
	if err != nil {
		t.Fatalf("ListInDir: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListInDir = %v, want empty", got)
	}
}

// TestListInDir_MissingDirectory pins the os.ReadDir error
// passthrough. We do not wrap the error: the caller is best placed
// to add user-facing context (e.g. mentioning the cwd).
func TestListInDir_MissingDirectory(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := ListInDir(missing)
	if err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

// TestListInDir_SkipsNonRegularFile mirrors TestResolve_NotRegularFile:
// a FIFO with a .md extension must be filtered out at the directory
// scan layer too so the picker never offers something that would
// blow up later inside Resolve.
func TestListInDir_SkipsNonRegularFile(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("posix-only fifo test")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	pipe := filepath.Join(dir, "pipe.md")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(pipe) })

	got := listInDirBasenames(t, dir)
	want := []string{"spec.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListInDir = %v, want %v", got, want)
	}
}
