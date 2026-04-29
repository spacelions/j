package settings

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

func runSettingsArgs(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := New()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestList_MissingDB(t *testing.T) {
	t.Chdir(t.TempDir())
	out, _, err := runSettingsArgs(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "no settings stored") {
		t.Fatalf("stdout = %q, want no settings", out)
	}
}

func TestList_EmptyDB(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	out, _, err := runSettingsArgs(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "no settings stored") {
		t.Fatalf("stdout = %q, want no settings", out)
	}
}

func TestList_PrintsSortedEntries(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
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

	out, _, err := runSettingsArgs(t)
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

// TestList_OpenError forces store.Open to fail: path exists as a
// directory so the DB cannot be opened.
func TestList_OpenError(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err = runSettingsArgs(t)
	if err == nil {
		t.Fatal("expected open error")
	}
}

// TestList_StatNonENOENT exercises a stat error that is not ErrNotExist.
func TestList_StatNonENOENT(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := store.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = runSettingsArgs(t)
	if err == nil {
		t.Fatal("expected stat error to propagate")
	}
}

// TestList_EmptyBucketsPath prints the same as missing keys: DB
// with only empty bucket names still lists no KVs, IsEmpty is true.
func TestList_OnlyEmptyBuckets(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket("ghost"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	out, _, err := runSettingsArgs(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "no settings stored") {
		t.Fatalf("stdout = %q, want no settings", out)
	}
}
