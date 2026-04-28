package work

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestResolveTarget_Markdown(t *testing.T) {
	dir := t.TempDir()
	for _, ext := range []string{".md", ".markdown"} {
		t.Run(ext, func(t *testing.T) {
			p := filepath.Join(dir, "spec"+ext)
			if err := os.WriteFile(p, []byte("hi"), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := resolveTarget(p)
			if err != nil {
				t.Fatalf("resolveTarget: %v", err)
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

func TestResolveTarget_TrimsAndUppercaseExtension(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "SPEC.MD")
	if err := os.WriteFile(p, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveTarget("  " + p + "  ")
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	want, _ := filepath.Abs(p)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveTarget_Errors(t *testing.T) {
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
		{"non-markdown", txt, "not a plan markdown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveTarget(tc.in)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func TestResolveTarget_NotRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only fifo test")
	}
	dir := t.TempDir()
	pipe := filepath.Join(dir, "spec.md")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(pipe) })

	_, err := resolveTarget(pipe)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("err = %v", err)
	}
}
