package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ExecutableStubOptions describes a PATH-resolvable shell stub that
// records argv as NUL-separated entries and emits canned stdout.
type ExecutableStubOptions struct {
	Binary    string
	Stdout    string
	ExitCode  int
	RecordCWD bool
}

// ExecutableStub is the set of files written by InstallExecutableStub.
type ExecutableStub struct {
	CallsPath string
	CWDPath   string
}

func InstallExecutableStub(
	t *testing.T, opts ExecutableStubOptions,
) ExecutableStub {
	t.Helper()
	dir := t.TempDir()
	stub := ExecutableStub{
		CallsPath: filepath.Join(dir, "calls.log"),
	}
	stdoutPath := filepath.Join(dir, "stdout.txt")
	if err := os.WriteFile(stdoutPath, []byte(opts.Stdout), 0o644); err != nil {
		t.Fatal(err)
	}
	cwdLine := ""
	if opts.RecordCWD {
		stub.CWDPath = filepath.Join(dir, "cwd.log")
		cwdLine = fmt.Sprintf("pwd > %q\n", stub.CWDPath)
	}
	body := fmt.Sprintf(`#!/bin/sh
: > %q
for a in "$@"; do printf '%%s\0' "$a" >> %q; done
%scat %q
exit %d
`, stub.CallsPath, stub.CallsPath, cwdLine, stdoutPath, opts.ExitCode)
	bin := filepath.Join(dir, opts.Binary)
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stub
}

func InstallPathScript(t *testing.T, binary, body string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, binary)
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin
}

func ReadNullArgsBestEffort(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return nil
	}
	return splitNullArgs(b)
}

func ReadNullArgs(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read calls.log: %v", err)
	}
	if len(b) == 0 {
		return nil
	}
	return splitNullArgs(b)
}

func splitNullArgs(b []byte) []string {
	parts := strings.Split(string(b), "\x00")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func WaitForNullArgs(
	t *testing.T, path string, want int, timeout time.Duration,
) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		argv := ReadNullArgsBestEffort(path)
		if len(argv) >= want {
			return argv
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %d argv entries at %s", want, path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func WaitForLog(
	t *testing.T, path, want string, timeout time.Duration,
) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), want) {
			return string(data)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %q in %s", want, path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func ReadTrimmedFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	return strings.TrimSpace(string(b))
}

func WaitForTrimmedFile(
	t *testing.T, path string, timeout time.Duration,
) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		b, err := os.ReadFile(path)
		if err == nil && len(strings.TrimSpace(string(b))) > 0 {
			return strings.TrimSpace(string(b))
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for contents at %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func NoopJBinary(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "j-stub.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func ArgvJBinary(t *testing.T, outputPath string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "j-argv-stub.sh")
	body := fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q.tmp && mv %q.tmp %q\n",
		outputPath, outputPath, outputPath,
	)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func ReadSpawnedArgv(t *testing.T, path string) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return strings.Split(strings.TrimSpace(string(data)), "\n")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("spawned argv was not written to %s", path)
	return nil
}

func InstallCursorAgentLoginStub(t *testing.T) {
	t.Helper()
	InstallPathScript(t, "cursor-agent",
		"#!/bin/sh\nprintf 'Logged in\\n'\nexit 0\n")
}
