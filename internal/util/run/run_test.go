package run

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestExec_OutputSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only smoke test")
	}
	out, err := NewExec().Output(context.Background(), "echo", "hi")
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if strings.TrimSpace(out) != "hi" {
		t.Fatalf("output = %q", out)
	}
}

func TestExec_OutputFailure_WithStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only smoke test")
	}
	// `ls /no/such/path` exits non-zero and writes a clear error to stderr
	// across BSD and GNU coreutils, so the wrapped message is exercised.
	_, err := NewExec().Output(context.Background(), "ls", "/no/such/path/should/not/exist")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ls") {
		t.Fatalf("err = %v", err)
	}
}

func TestExec_OutputFailure_StdoutOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only smoke test")
	}
	// A shell snippet that fails non-zero and writes to stdout but not
	// stderr exercises the stderr-empty-but-stdout-nonempty fallback.
	_, err := NewExec().Output(context.Background(), "sh", "-c", "echo stdoutmsg; exit 1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stdoutmsg") {
		t.Fatalf("err = %v", err)
	}
}

func TestExec_OutputFailure_NoStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only smoke test")
	}
	// `false` exits non-zero with no stdout/stderr, exercising the
	// both-empty fallback path.
	_, err := NewExec().Output(context.Background(), "false")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "false") {
		t.Fatalf("err = %v", err)
	}
}

func TestExec_Run_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only smoke test")
	}
	// `true` inherits stdin/stdout/stderr (so nothing is written) and
	// exits zero; exercises the success path of execRunner.Run.
	if err := NewExec().Run(context.Background(), "true"); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestExec_Run_Failure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only smoke test")
	}
	// `false` exits non-zero with no output; exercises Run's error wrap.
	err := NewExec().Run(context.Background(), "false")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "false") {
		t.Fatalf("err = %v", err)
	}
}
