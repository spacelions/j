package settings

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/store"
)

func runResetArgs(t *testing.T, in io.Reader, args ...string) (string, error) {
	t.Helper()
	cmd := New()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if in == nil {
		in = &bytes.Buffer{}
	}
	cmd.SetIn(in)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String() + stderr.String(), err
}

func TestReset_Full_MissingJDir(t *testing.T) {
	t.Chdir(t.TempDir())
	out, err := runResetArgs(t, &bytes.Buffer{}, "reset")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "nothing to reset") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestReset_Full_EmptyJ(t *testing.T) {
	t.Chdir(t.TempDir())
	jDir, err := store.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jDir, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runResetArgs(t, &bytes.Buffer{}, "reset")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "nothing to reset") {
		t.Fatalf("stdout = %q, want nothing to reset", out)
	}
}

func TestReset_Full_YesRemovesJ(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := runSetArgs(t, "set", "a.k", "v"); err != nil {
		t.Fatalf("set: %v", err)
	}
	jDir, err := store.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jDir); err != nil {
		t.Fatalf(".j: %v", err)
	}
	out, err := runResetArgs(t, &bytes.Buffer{}, "reset", "-y")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(out, "removed "+jDir) {
		t.Fatalf("stdout = %q, want line with %q", out, jDir)
	}
	if _, err := os.Stat(jDir); !os.IsNotExist(err) {
		t.Fatalf(".j should be gone, stat: %v", err)
	}
}

func TestReset_Full_StdinYes(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := runSetArgs(t, "set", "a.k", "v"); err != nil {
		t.Fatalf("set: %v", err)
	}
	_, err := runResetArgs(t, bytes.NewBufferString("yes\n"), "reset")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	jDir, _ := store.DefaultDir()
	if _, err := os.Stat(jDir); !os.IsNotExist(err) {
		t.Fatal("expected .j removed")
	}
}

func TestReset_Full_StdinY(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := runSetArgs(t, "set", "a.k", "v"); err != nil {
		t.Fatalf("set: %v", err)
	}
	_, err := runResetArgs(t, bytes.NewBufferString("y\n"), "reset")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	jDir, _ := store.DefaultDir()
	if _, err := os.Stat(jDir); !os.IsNotExist(err) {
		t.Fatal("expected .j removed")
	}
}

func TestReset_Full_StdinNo(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := runSetArgs(t, "set", "a.k", "v"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := runResetArgs(t, bytes.NewBufferString("n\n"), "reset")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(out, "reset aborted") {
		t.Fatalf("stdout = %q", out)
	}
	p, _ := store.DefaultPath()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("db should still exist: %v", err)
	}
}

func TestReset_Single_MissingDB(t *testing.T) {
	t.Chdir(t.TempDir())
	out, err := runResetArgs(t, &bytes.Buffer{}, "reset", "a.b")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "nothing to reset") {
		t.Fatalf("out = %q", out)
	}
}

func TestReset_Single_RemovesValue(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := runSetArgs(t, "set", "b.k1", "x"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := runSetArgs(t, "set", "b.k2", "y"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := runResetArgs(t, &bytes.Buffer{}, "reset", "b.k1")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(out, "unset b.k1") {
		t.Fatalf("out = %q", out)
	}
	p, _ := store.DefaultPath()
	s, err := store.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	_, ok, err := s.Get("b", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("k1 should be gone")
	}
	v, ok, err := s.Get("b", "k2")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || v != "y" {
		t.Fatalf("k2: got %q ok=%v", v, ok)
	}
}

func TestReset_Single_MissingKeyStillOK(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := runSetArgs(t, "set", "b.k2", "y"); err != nil {
		t.Fatalf("set: %v", err)
	}
	_, err := runResetArgs(t, &bytes.Buffer{}, "reset", "b.ghost")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
}

func TestReset_Single_BadKey(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := runResetArgs(t, &bytes.Buffer{}, "reset", "nodot")
	if err == nil {
		t.Fatal("expected error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read error") }

func TestReadConfirmationLine_ReadError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(errReader{})
	if _, err := readConfirmationLine(cmd); err == nil {
		t.Fatal("expected read error")
	}
}

// TestRunResetOneKey_StatError exercises the non-ENOENT stat error path
// (same shape as the list path when .j is a file).
func TestRunResetOneKey_StatError(t *testing.T) {
	t.Chdir(t.TempDir())
	d, err := store.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = runResetArgs(t, &bytes.Buffer{}, "reset", "a.b")
	if err == nil {
		t.Fatal("expected error")
	}
}
