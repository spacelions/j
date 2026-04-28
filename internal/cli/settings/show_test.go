package settings

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

func runShowArgs(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := New()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestShow_MissingDB(t *testing.T) {
	t.Chdir(t.TempDir())
	out, _, err := runShowArgs(t, "show")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "j settings init") {
		t.Fatalf("stdout = %q, want hint", out)
	}
}

func TestShow_EmptyDB(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, _, err := runShowArgs(t, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Wipe buckets to simulate a brand-new DB without any pre-created
	// bucket — exercises the "no settings stored" notice.
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	out, _, err := runShowArgs(t, "show")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "no settings stored") {
		t.Fatalf("stdout = %q, want empty notice", out)
	}
}

func TestShow_PrintsSortedEntries(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, _, err := runShowArgs(t, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Seed both planner and coder buckets so the test pins the
	// cross-bucket ordering (buckets sorted, then keys sorted within
	// each bucket). "zeta" stays in to keep an arbitrary third bucket
	// asserting the sort order is not just two-way.
	for k, v := range map[string]string{
		"tool":  "cursor",
		"model": "sonnet-4",
	} {
		if err := s.Put(store.BucketPlanner, k, v); err != nil {
			t.Fatal(err)
		}
	}
	for k, v := range map[string]string{
		"tool":        "cursor",
		"model":       "gpt-5",
		"interactive": "false",
	} {
		if err := s.Put(store.BucketCoder, k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Put("zeta", "k", "v"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	out, _, err := runShowArgs(t, "show")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "coder.interactive = false\n" +
		"coder.model = gpt-5\n" +
		"coder.tool = cursor\n" +
		"planner.model = sonnet-4\n" +
		"planner.tool = cursor\n" +
		"zeta.k = v\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

// TestShow_OpenError forces store.Open to fail by replacing the DB file
// with a directory before show runs. This also exercises the path where
// os.Stat succeeds (the directory exists) but the DB cannot be opened.
func TestShow_OpenError(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runShowArgs(t, "show"); err == nil {
		t.Fatal("expected open error")
	}
}

// TestShow_StatNonENOENT exercises the non-ErrNotExist branch in
// runShow's os.Stat block. We make `<cwd>/.j` a regular file so that
// stat on `<cwd>/.j/settings` returns ENOTDIR (not ENOENT), forcing
// runShow to surface the error rather than print the "missing DB" hint.
func TestShow_StatNonENOENT(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := store.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	// Replace what would be the .j directory with a regular file so
	// the traversal to .j/settings fails with ENOTDIR.
	if err := os.WriteFile(dir, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = runShowArgs(t, "show")
	if err == nil {
		t.Fatal("expected stat error to propagate")
	}
}
