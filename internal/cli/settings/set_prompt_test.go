package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/instructions"
	"github.com/spacelions/j/internal/store"
)

// promptCases pairs each role bucket with its embedded markdown body
// so the table-driven tests below exercise all three in one shot.
var promptCases = []struct {
	bucket string
	body   string
}{
	{store.BucketPlanner, instructions.Planner},
	{store.BucketWorker, instructions.Worker},
	{store.BucketVerifier, instructions.Verifier},
}

// TestSet_PromptCopyOnSet exercises the copy-on-set hook for each role
// prompt: a missing destination file is seeded with the bundled
// markdown, parents are created, and the override path lands in the
// settings store.
func TestSet_PromptCopyOnSet(t *testing.T) {
	for _, tc := range promptCases {
		t.Run(tc.bucket, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			mustInit(t)
			dest := filepath.Join(dir, "prompts", "nested", tc.bucket+".md")
			arg := tc.bucket + ".prompt=" + dest
			out, err := runSetArgs(t, "set", arg)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !strings.Contains(out, "wrote default prompt to "+dest) {
				t.Fatalf("stdout = %q, want copy line", out)
			}
			if !strings.Contains(out, "set "+tc.bucket+".prompt = "+dest) {
				t.Fatalf("stdout = %q, want set line", out)
			}
			body, err := os.ReadFile(dest)
			if err != nil {
				t.Fatalf("read seeded prompt: %v", err)
			}
			if string(body) != tc.body {
				t.Fatalf("seeded body mismatch for %s", tc.bucket)
			}
			assertStoredPath(t, tc.bucket, dest)
		})
	}
}

// TestSet_PromptExistingFilePreserved confirms an existing file is
// NOT overwritten when the path already exists; only the store entry
// is updated.
func TestSet_PromptExistingFilePreserved(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	mustInit(t)
	dest := filepath.Join(dir, "my_planner.md")
	const original = "user-edited body do not touch"
	if err := os.WriteFile(dest, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	out, err := runSetArgs(t, "set", "planner.prompt="+dest)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "wrote default prompt") {
		t.Fatalf("should not write when file exists: %q", out)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != original {
		t.Fatalf("body changed: got %q, want %q", body, original)
	}
	assertStoredPath(t, store.BucketPlanner, dest)
}

// TestSet_PromptNonRoleBucketUnaffected confirms `<other>.prompt`
// keys are NOT seeded — only planner / worker / verifier qualify.
// The store entry is still written, mirroring today's behaviour for
// arbitrary bucket.key pairs.
func TestSet_PromptNonRoleBucketUnaffected(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	mustInit(t)
	dest := filepath.Join(dir, "ghost.md")
	out, err := runSetArgs(t, "set", "ghost.prompt="+dest)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "wrote default prompt") {
		t.Fatalf("non-role bucket should not seed file: %q", out)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("non-role bucket created file: stat err = %v", err)
	}
	assertStoredPath(t, "ghost", dest)
}

// TestSet_PromptNonPromptKeyUnaffected confirms `<role>.<other>` keys
// (e.g. planner.tool) skip the copy-on-set path entirely.
func TestSet_PromptNonPromptKeyUnaffected(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	mustInit(t)
	out, err := runSetArgs(t, "set", "planner.tool=cursor")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "wrote default prompt") {
		t.Fatalf("non-prompt key should not seed file: %q", out)
	}
}

// TestSet_PromptRelativePathCopiesUnderCwd confirms a relative path
// is resolved against the current working directory so the seeded
// file lands under `dir/<rel>` even though only the relative path is
// stored.
func TestSet_PromptRelativePathCopiesUnderCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	mustInit(t)
	const rel = "rel/worker.md"
	if _, err := runSetArgs(t, "set", "worker.prompt="+rel); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read seeded file: %v", err)
	}
	if string(body) != instructions.Worker {
		t.Fatalf("seeded body mismatch")
	}
}

// TestSet_PromptStatErrorPropagates pins the defensive branch where
// stat fails with a non-ENOENT error: the helper returns the wrapped
// error rather than masking it. We force ENOTDIR by placing the
// destination under a regular file (which os.Stat reports as a
// directory traversal failure, not a missing file).
func TestSet_PromptStatErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	mustInit(t)
	notADir := filepath.Join(dir, "blocker")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	dest := filepath.Join(notADir, "child.md")
	_, err := runSetArgs(t, "set", "planner.prompt="+dest)
	if err == nil {
		t.Fatal("expected stat error")
	}
	if !strings.Contains(err.Error(), "stat") {
		t.Fatalf("err = %v, want stat-wrapped error", err)
	}
}

func assertStoredPath(t *testing.T, bucket, want string) {
	t.Helper()
	dbPath, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	got, ok, err := s.Get(bucket, store.KeyPromptPath)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatalf("Get(%s.prompt): missing", bucket)
	}
	if got != want {
		t.Fatalf("stored = %q, want %q", got, want)
	}
}
