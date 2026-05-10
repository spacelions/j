package settings

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/testutil"
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
	testutil.Init(t)
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

// TestList_EmptyDB pins the listing after mustInit: the seeded row is
// the project.must_read placeholder; the four known sections always render in
// fixed order even when empty.
func TestList_EmptyDB(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)

	out, _, err := runSettingsArgs(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "[project]\n" +
		"  must_read = \n" +
		"\n" +
		"[planner]\n" +
		"\n" +
		"[worker]\n" +
		"\n" +
		"[verifier]\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
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
		if err := s.Put(store.BucketWorker, k, v); err != nil {
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
	want := "[project]\n" +
		"  must_read = \n" +
		"\n" +
		"[planner]\n" +
		"  model = sonnet-4\n" +
		"  tool = cursor\n" +
		"\n" +
		"[worker]\n" +
		"  interactive = false\n" +
		"  model = gpt-5\n" +
		"  tool = cursor\n" +
		"\n" +
		"[verifier]\n" +
		"\n" +
		"[zeta]\n" +
		"  k = v\n"
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

// TestList_MasksAPIKey pins the secret-redaction behaviour: when the
// project bucket carries an `api_key` entry, the list output renders
// it as the fixed mask `****` so the real value never echoes onto the
// terminal. Other project keys (model, max_iterations, must_read) are
// rendered verbatim alongside it.
func TestList_MasksAPIKey(t *testing.T) {
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
		"api_key":        "secret123",
		"model":          "gemini-2.5-flash",
		"max_iterations": "3",
	} {
		if err := s.Put(store.BucketProject, k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	out, _, err := runSettingsArgs(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "api_key = ****") {
		t.Fatalf("stdout = %q, want masked api_key line", out)
	}
	if strings.Contains(out, "secret123") {
		t.Fatalf("stdout = %q, must not echo the real api_key", out)
	}
	if !strings.Contains(out, "model = gemini-2.5-flash") {
		t.Fatalf("stdout = %q, want model rendered verbatim", out)
	}
	if !strings.Contains(out, "max_iterations = 3") {
		t.Fatalf("stdout = %q, want max_iterations rendered verbatim", out)
	}
}

// TestList_MasksLinearAPIKey pins the secret-redaction behaviour
// for the linear bucket: api_key renders as `****` while
// linear.project renders verbatim.
func TestList_MasksLinearAPIKey(t *testing.T) {
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
	if err := s.Put(store.BucketLinear, store.KeyLinearAPIKey, "lin_api_secret"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketLinear, store.KeyLinearProject, "project-id"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	out, _, err := runSettingsArgs(t)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "api_key = ****") {
		t.Fatalf("stdout = %q, want masked api_key line", out)
	}
	if strings.Contains(out, "lin_api_secret") {
		t.Fatalf("stdout = %q, must not echo linear api key", out)
	}
	if !strings.Contains(out, "project = project-id") {
		t.Fatalf("stdout = %q, want linear.project rendered verbatim", out)
	}
}

// TestList_OnlyEmptyBuckets verifies the unknown-empty-bucket skip:
// a DB whose only bucket is empty and unknown renders just the four
// known section headers, with no entries and no [ghost] section.
// Bypasses the cobra preflight (which would otherwise seed
// project.must_read and pollute the "only empty buckets" premise).
func TestList_OnlyEmptyBuckets(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
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
	out, err := runListBare(t)
	if err != nil {
		t.Fatalf("runList: %v", err)
	}
	want := "[project]\n" +
		"\n" +
		"[planner]\n" +
		"\n" +
		"[worker]\n" +
		"\n" +
		"[verifier]\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestCollectSections_ListBucketsError(t *testing.T) {
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
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := collectSections(s); err == nil {
		t.Fatal("collectSections error = nil")
	}
}

func TestPrintSections_ListError(t *testing.T) {
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
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	err = printSections(
		&bytes.Buffer{},
		[]string{store.BucketProject},
		map[string]bool{store.BucketProject: true},
		s,
	)
	if err == nil {
		t.Fatal("printSections error = nil")
	}
}
