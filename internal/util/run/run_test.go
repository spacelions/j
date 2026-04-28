//go:build !windows

package run

import (
	"context"
	"strings"
	"testing"
)

func TestOutput_Success(t *testing.T) {
	out, err := Output(context.Background(), "echo", "hi")
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if strings.TrimSpace(out) != "hi" {
		t.Fatalf("output = %q", out)
	}
}

func TestOutput_Failure_WithStderr(t *testing.T) {
	// `ls /no/such/path` exits non-zero and writes a clear error to
	// stderr across BSD and GNU coreutils, so the wrapped message is
	// exercised.
	_, err := Output(context.Background(), "ls", "/no/such/path/should/not/exist")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ls") {
		t.Fatalf("err = %v", err)
	}
}

func TestOutput_Failure_StdoutOnly(t *testing.T) {
	// A shell snippet that fails non-zero and writes to stdout but not
	// stderr exercises the stderr-empty-but-stdout-nonempty fallback.
	_, err := Output(context.Background(), "sh", "-c", "echo stdoutmsg; exit 1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stdoutmsg") {
		t.Fatalf("err = %v", err)
	}
}

func TestOutput_Failure_NoStderr(t *testing.T) {
	// `false` exits non-zero with no stdout/stderr, exercising the
	// both-empty fallback path.
	_, err := Output(context.Background(), "false")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "false") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_Success(t *testing.T) {
	// `true` inherits stdin/stdout/stderr (so nothing is written) and
	// exits zero; exercises the success path of Run.
	if err := Run(context.Background(), "true"); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRun_Failure(t *testing.T) {
	// `false` exits non-zero with no output; exercises Run's error wrap.
	err := Run(context.Background(), "false")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "false") {
		t.Fatalf("err = %v", err)
	}
}
