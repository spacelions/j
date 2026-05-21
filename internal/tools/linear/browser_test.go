package linear

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenURL_StubReplaceable(t *testing.T) {
	prev := OpenURL
	t.Cleanup(func() { OpenURL = prev })
	called := ""
	OpenURL = func(url string) error {
		called = url
		return nil
	}
	if err := OpenURL("https://example/x"); err != nil {
		t.Fatalf("OpenURL: %v", err)
	}
	if called != "https://example/x" {
		t.Fatalf("called = %q, want stubbed argument", called)
	}
}

func TestOpenURL_StubError(t *testing.T) {
	prev := OpenURL
	t.Cleanup(func() { OpenURL = prev })
	want := errors.New("boom")
	OpenURL = func(string) error { return want }
	if err := OpenURL("https://x"); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

// stubBrowserBinary writes a shell script onto PATH that takes the
// place of `open` (darwin) or `xdg-open` (linux) for openURL. exitCode
// controls the script's exit status so callers can drive both the
// success and failure branches.
func stubBrowserBinary(t *testing.T, exitCode int) {
	t.Helper()
	dir := t.TempDir()
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	script := "#!/bin/sh\nexit " + map[int]string{0: "0", 1: "1"}[exitCode] + "\n"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestOpenURL_DefaultSuccess exercises the real (non-stubbed) openURL
// against a PATH-resolvable fake `open`/`xdg-open` script.
func TestOpenURL_DefaultSuccess(t *testing.T) {
	stubBrowserBinary(t, 0)
	if err := openURL("https://example.invalid/"); err != nil {
		t.Fatalf("openURL: %v", err)
	}
}

// TestOpenURL_DefaultFailure drives the cmd.Run() error branch via a
// fake browser launcher that exits non-zero.
func TestOpenURL_DefaultFailure(t *testing.T) {
	stubBrowserBinary(t, 1)
	err := openURL("https://example.invalid/")
	if err == nil {
		t.Fatal("openURL should propagate non-zero exit")
	}
}
