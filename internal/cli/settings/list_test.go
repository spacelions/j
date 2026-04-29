package settings

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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

// runListBare exercises the plain `j settings` list path WITHOUT
// going through the cobra wiring (and therefore without pre-flight).
// Tests use it to drive the defensive branches in runList that the
// pre-flight contract would otherwise hide.
func runListBare(t *testing.T) (string, error) {
	t.Helper()
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	err := runList(cmd)
	return stdout.String(), err
}

// mustInit lays down the .j layout in the current working directory.
// Tests must call this helper after t.Chdir so the new pre-flight
// contract is satisfied (otherwise the j settings command intercepts
// with the init prompt). Idempotent.
func mustInit(t *testing.T) {
	t.Helper()
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
}

// TestList_MissingDB pins the defense-in-depth branch in runList: a
// missing settings file prints "no settings stored" instead of
// surfacing a stat error. Pre-flight normally heals this state; we
// bypass cobra to keep the file missing while exercising runList.
func TestList_MissingDB(t *testing.T) {
	t.Chdir(t.TempDir())
	out, err := runListBare(t)
	if err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(out, "no settings stored") {
		t.Fatalf("stdout = %q, want no settings", out)
	}
}

func TestList_EmptyDB(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)

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
	mustInit(t)
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

// TestList_OpenError forces store.Open to fail: the settings path
// exists as a directory so bolt.Open cannot open it. The test
// bypasses cobra (and pre-flight) so the corrupt layout reaches
// runList unchanged.
func TestList_OpenError(t *testing.T) {
	t.Chdir(t.TempDir())
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runListBare(t); err == nil {
		t.Fatal("expected open error")
	}
}

// TestList_StatNonENOENT exercises a stat error that is not
// ErrNotExist: a file at the .j path makes the parent stat fail. The
// test bypasses cobra so the corrupt layout survives pre-flight.
func TestList_StatNonENOENT(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := store.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runListBare(t); err == nil {
		t.Fatal("expected stat error to propagate")
	}
}

// TestList_OnlyEmptyBuckets prints the same as missing keys: a DB
// with only empty bucket names still lists no KVs, IsEmpty is true.
func TestList_OnlyEmptyBuckets(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
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
