package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultPath_RootedInCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	wantDir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	resolvedGotDir, err := filepath.EvalSymlinks(filepath.Dir(wantDir))
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if resolvedGotDir != resolvedDir {
		t.Fatalf("DefaultDir parent = %q, want %q", resolvedGotDir, resolvedDir)
	}
	if filepath.Base(wantDir) != ".j" {
		t.Fatalf("DefaultDir base = %q, want .j", filepath.Base(wantDir))
	}
	if filepath.Base(got) != "settings" {
		t.Fatalf("DefaultPath base = %q, want settings", filepath.Base(got))
	}
	if filepath.Dir(got) != wantDir {
		t.Fatalf("DefaultPath dir = %q, want %q", filepath.Dir(got), wantDir)
	}
}

// TestDefaultDir_CwdRemoved exercises the os.Getwd failure path by
// chdir-ing into a temp dir, removing it, and clearing PWD so Go's
// getwd helper has to fall back to the syscall (which fails). On
// systems where the kernel reconstructs the path via cached inodes
// (macOS, some FUSE setups) the syscall still succeeds, in which case
// the test skips rather than failing - the same line is exercised in
// CI on Linux where the failure is deterministic.
func TestDefaultDir_CwdRemoved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cwd cannot be removed while in use on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root may bypass relevant FS errors")
	}
	parent := t.TempDir()
	gone := filepath.Join(parent, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	t.Setenv("PWD", "")
	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if _, err := DefaultDir(); err == nil {
		t.Skip("os.Getwd unexpectedly succeeded after cwd removal; cannot exercise failure path on this OS")
	}
	if _, err := DefaultPath(); err == nil {
		t.Fatal("DefaultPath should propagate DefaultDir error")
	}
}

func TestOpen_EmptyPath(t *testing.T) {
	if _, err := Open(""); err == nil || !strings.Contains(err.Error(), "empty path") {
		t.Fatalf("err = %v, want empty path error", err)
	}
}

// TestOpen_RequiresExistingParent pins the new Open contract: the
// helper no longer mkdir-as the parent. A nested path with a missing
// parent surfaces the underlying bolt.Open error.
func TestOpen_RequiresExistingParent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "child", "settings")
	if _, err := Open(path); err == nil {
		t.Fatal("Open should fail when parent dir is missing")
	}
}

// TestOpen_OpensExistingPath confirms Open succeeds when EnsureProject
// has already laid down the parent directory and bbolt file.
func TestOpen_OpensExistingPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
}

// TestOpen_BoltOpenFails forces bolt.Open to fail by pointing path at
// an existing directory; bolt cannot open a directory as a file.
func TestOpen_BoltOpenFails(t *testing.T) {
	root := t.TempDir()
	if _, err := Open(root); err == nil {
		t.Fatal("expected bolt open error")
	}
}

func TestStore_CloseNil(t *testing.T) {
	var s *Store
	if err := s.Close(); err != nil {
		t.Fatalf("nil-receiver Close should be no-op: %v", err)
	}
	empty := &Store{}
	if err := empty.Close(); err != nil {
		t.Fatalf("empty Close: %v", err)
	}
}

// openInTemp chdirs into a fresh temp dir, runs EnsureProject so the
// .j layout is in place, opens the settings DB, and registers a
// Cleanup to close it. Tests that need to drive Store methods reach
// for this helper.
func openInTemp(t *testing.T) *Store {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestEnsureBucket_Idempotent(t *testing.T) {
	s := openInTemp(t)
	if err := s.EnsureBucket(BucketPlanner); err != nil {
		t.Fatalf("first EnsureBucket: %v", err)
	}
	if err := s.EnsureBucket(BucketPlanner); err != nil {
		t.Fatalf("second EnsureBucket: %v", err)
	}
	buckets, err := s.ListBuckets()
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	if len(buckets) != 1 || buckets[0] != BucketPlanner {
		t.Fatalf("buckets = %v", buckets)
	}
}

func TestPutGet_RoundTrip(t *testing.T) {
	s := openInTemp(t)
	if err := s.Put(BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	val, ok, err := s.Get(BucketPlanner, "tool")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || val != "cursor" {
		t.Fatalf("Get = (%q, %v), want (cursor, true)", val, ok)
	}
}

func TestGet_MissingBucket(t *testing.T) {
	s := openInTemp(t)
	val, ok, err := s.Get("nope", "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok || val != "" {
		t.Fatalf("Get = (%q, %v), want (\"\", false)", val, ok)
	}
}

func TestGet_MissingKey(t *testing.T) {
	s := openInTemp(t)
	if err := s.EnsureBucket(BucketPlanner); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	val, ok, err := s.Get(BucketPlanner, "absent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok || val != "" {
		t.Fatalf("Get = (%q, %v), want (\"\", false)", val, ok)
	}
}

// TestPut_EmptyBucketName exercises the CreateBucketIfNotExists error
// branch in Put. bbolt rejects empty bucket names with ErrBucketNameRequired.
func TestPut_EmptyBucketName(t *testing.T) {
	s := openInTemp(t)
	if err := s.Put("", "k", "v"); err == nil {
		t.Fatal("Put with empty bucket name should error")
	}
}

// TestEnsureBucket_EmptyName covers the same error path through
// EnsureBucket so its branch counter is exercised.
func TestEnsureBucket_EmptyName(t *testing.T) {
	s := openInTemp(t)
	if err := s.EnsureBucket(""); err == nil {
		t.Fatal("EnsureBucket with empty name should error")
	}
}

func TestPutGet_EmptyValue(t *testing.T) {
	s := openInTemp(t)
	if err := s.Put(BucketPlanner, "k", ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	val, ok, err := s.Get(BucketPlanner, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || val != "" {
		t.Fatalf("Get = (%q, %v), want (\"\", true)", val, ok)
	}
}

func TestList_EmptyBucket(t *testing.T) {
	s := openInTemp(t)
	if err := s.EnsureBucket(BucketPlanner); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	got, err := s.List(BucketPlanner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List on empty = %v", got)
	}
}

func TestList_MissingBucket(t *testing.T) {
	s := openInTemp(t)
	got, err := s.List("nope")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List on missing bucket = %v", got)
	}
}

func TestList_SortedByKey(t *testing.T) {
	s := openInTemp(t)
	keys := []string{"zeta", "alpha", "mu", "beta"}
	for _, k := range keys {
		if err := s.Put(BucketPlanner, k, "v-"+k); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.List(BucketPlanner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	wantOrder := []string{"alpha", "beta", "mu", "zeta"}
	if len(got) != len(wantOrder) {
		t.Fatalf("List len = %d, want %d", len(got), len(wantOrder))
	}
	for i, kv := range got {
		if kv.Key != wantOrder[i] {
			t.Fatalf("got[%d].Key = %q, want %q", i, kv.Key, wantOrder[i])
		}
		if kv.Value != "v-"+wantOrder[i] {
			t.Fatalf("got[%d].Value = %q", i, kv.Value)
		}
	}
}

func TestListBuckets_Sorted(t *testing.T) {
	s := openInTemp(t)
	for _, b := range []string{"zeta", "alpha", "mu"} {
		if err := s.EnsureBucket(b); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListBuckets()
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	want := []string{"alpha", "mu", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestListBuckets_EmptyDB(t *testing.T) {
	s := openInTemp(t)
	got, err := s.ListBuckets()
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListBuckets on empty = %v", got)
	}
}

// TestStore_OperationsAfterClose drives the error paths in Put/Get/List/
// EnsureBucket/ListBuckets by invoking them on a closed DB.
func TestStore_OperationsAfterClose(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatal(err)
	}
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := s.EnsureBucket(BucketPlanner); err == nil {
		t.Fatal("EnsureBucket on closed db should error")
	}
	if err := s.Put(BucketPlanner, "k", "v"); err == nil {
		t.Fatal("Put on closed db should error")
	}
	if _, _, err := s.Get(BucketPlanner, "k"); err == nil {
		t.Fatal("Get on closed db should error")
	}
	if _, err := s.List(BucketPlanner); err == nil {
		t.Fatal("List on closed db should error")
	}
	if _, err := s.ListBuckets(); err == nil {
		t.Fatal("ListBuckets on closed db should error")
	}
	if err := s.Delete(BucketPlanner, "k"); err == nil {
		t.Fatal("Delete on closed db should error")
	}
	if err := s.DeleteBucket(BucketPlanner); err == nil {
		t.Fatal("DeleteBucket on closed db should error")
	}
	if _, err := s.IsEmpty(); err == nil {
		t.Fatal("IsEmpty on closed db should error")
	}
}

func TestDelete_MissingBucket(t *testing.T) {
	s := openInTemp(t)
	if err := s.Delete("nope", "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestDelete_MissingKey(t *testing.T) {
	s := openInTemp(t)
	if err := s.EnsureBucket(BucketPlanner); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(BucketPlanner, "absent"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestDelete_RemovesKey(t *testing.T) {
	s := openInTemp(t)
	if err := s.Put(BucketPlanner, "k", "v"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(BucketPlanner, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, ok, err := s.Get(BucketPlanner, "k")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("key should be gone")
	}
}

// TestDeleteBucket_RemovesEveryKey pins the contract: deleting a
// populated bucket wipes every key inside it and the bucket itself
// so List/ListBuckets stop reporting it.
func TestDeleteBucket_RemovesEveryKey(t *testing.T) {
	s := openInTemp(t)
	for _, k := range []string{"tool", "model", "interactive"} {
		if err := s.Put(BucketPlanner, k, "v-"+k); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteBucket(BucketPlanner); err != nil {
		t.Fatalf("DeleteBucket: %v", err)
	}
	got, err := s.List(BucketPlanner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List after DeleteBucket = %v, want empty", got)
	}
	buckets, err := s.ListBuckets()
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	for _, b := range buckets {
		if b == BucketPlanner {
			t.Fatalf("ListBuckets still contains %q after DeleteBucket: %v", BucketPlanner, buckets)
		}
	}
}

// TestDeleteBucket_MissingBucketIsNoop pins the missing-bucket
// no-op semantics that mirror Delete's missing-key behaviour: a
// caller may ask for a wipe without pre-checking existence.
func TestDeleteBucket_MissingBucketIsNoop(t *testing.T) {
	s := openInTemp(t)
	if err := s.DeleteBucket("never-created"); err != nil {
		t.Fatalf("DeleteBucket on missing bucket should be a no-op, got: %v", err)
	}
}

// TestDeleteBucket_EnsureBucketRecreates pins the round-trip: after
// DeleteBucket a subsequent EnsureBucket re-creates a clean (empty)
// bucket without surfacing the prior contents.
func TestDeleteBucket_EnsureBucketRecreates(t *testing.T) {
	s := openInTemp(t)
	if err := s.Put(BucketPlanner, "tool", "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBucket(BucketPlanner); err != nil {
		t.Fatalf("DeleteBucket: %v", err)
	}
	if err := s.EnsureBucket(BucketPlanner); err != nil {
		t.Fatalf("EnsureBucket after DeleteBucket: %v", err)
	}
	got, err := s.List(BucketPlanner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List after recreate = %v, want empty", got)
	}
}

func TestIsEmpty_NoBuckets(t *testing.T) {
	s := openInTemp(t)
	empty, err := s.IsEmpty()
	if err != nil {
		t.Fatalf("IsEmpty: %v", err)
	}
	if !empty {
		t.Fatal("want empty")
	}
}

func TestIsEmpty_OnlyEmptyBuckets(t *testing.T) {
	s := openInTemp(t)
	if err := s.EnsureBucket("a"); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket("b"); err != nil {
		t.Fatal(err)
	}
	empty, err := s.IsEmpty()
	if err != nil {
		t.Fatalf("IsEmpty: %v", err)
	}
	if !empty {
		t.Fatal("want empty when all buckets have no keys")
	}
}

func TestIsEmpty_NonEmpty(t *testing.T) {
	s := openInTemp(t)
	if err := s.Put(BucketPlanner, "k", "v"); err != nil {
		t.Fatal(err)
	}
	empty, err := s.IsEmpty()
	if err != nil {
		t.Fatalf("IsEmpty: %v", err)
	}
	if empty {
		t.Fatal("want non-empty")
	}
}


// TestEnsureProject_FreshDirCreatesAllArtifacts pins the
// initialisation contract: a brand-new directory becomes a fully-
// initialized project after a single EnsureProject call. The three
// artifacts (`.j`, `.j/settings`, `.j/tasks/`) must exist with the
// right type. Per-task `<id>/task.toml` files are NOT pre-created;
// they appear on demand as `j tasks start` mints rows.
func TestEnsureProject_FreshDirCreatesAllArtifacts(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	jdir := filepath.Join(dir, ".j")
	if info, err := os.Stat(jdir); err != nil || !info.IsDir() {
		t.Fatalf(".j dir missing: err=%v", err)
	}
	tasksDir := filepath.Join(jdir, tasksDirName)
	if info, err := os.Stat(tasksDir); err != nil || !info.IsDir() {
		t.Fatalf(".j/tasks dir missing: err=%v", err)
	}
	settingsPath := filepath.Join(jdir, "settings")
	if info, err := os.Stat(settingsPath); err != nil || info.IsDir() {
		t.Fatalf(".j/settings file missing: err=%v", err)
	}
}

// TestEnsureProject_Idempotent re-runs EnsureProject on a fully
// initialized project and asserts it stays a no-op (no error, files
// remain in place).
func TestEnsureProject_Idempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("first EnsureProject: %v", err)
	}
	if err := EnsureProject(); err != nil {
		t.Fatalf("second EnsureProject: %v", err)
	}
	ok, err := ProjectInitialized()
	if err != nil {
		t.Fatalf("ProjectInitialized: %v", err)
	}
	if !ok {
		t.Fatal("project should still be initialized after re-run")
	}
}

// TestEnsureProject_PartialState completes the layout when only some
// artifacts exist.
func TestEnsureProject_PartialState(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jdir := filepath.Join(dir, ".j")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Settings file present, tasks dir missing.
	if err := os.WriteFile(filepath.Join(jdir, "settings"), []byte("dummy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	ok, err := ProjectInitialized()
	if err != nil {
		t.Fatalf("ProjectInitialized: %v", err)
	}
	if !ok {
		t.Fatal("project should be initialized after partial-state ensure")
	}
}

// TestEnsureProject_AppendsToExistingGitignore pins the .gitignore
// allowlist behavior: an existing file gains the `.j` line.
func TestEnsureProject_AppendsToExistingGitignore(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gi := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gi, []byte("node_modules/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	got, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "node_modules/\n.j\n"
	if string(got) != want {
		t.Fatalf("gitignore = %q, want %q", string(got), want)
	}
}

// TestEnsureProject_GitignoreWithoutTrailingNewline preserves the
// existing append-with-newline-prefix behavior on dirty files.
func TestEnsureProject_GitignoreWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gi := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gi, []byte("node_modules/"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	got, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "node_modules/\n.j\n"
	if string(got) != want {
		t.Fatalf("gitignore = %q, want %q", string(got), want)
	}
}

// TestEnsureProject_GitignoreAlreadyHasJEntry leaves a .gitignore
// that already mentions .j unchanged.
func TestEnsureProject_GitignoreAlreadyHasJEntry(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gi := filepath.Join(dir, ".gitignore")
	original := "node_modules/\n.j\nbuild/\n"
	if err := os.WriteFile(gi, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	got, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != original {
		t.Fatalf("gitignore changed: got %q, want %q", string(got), original)
	}
}

// TestEnsureProject_GitignoreAlreadyHasJSlashEntry covers the .j/
// variant and surrounding whitespace tolerance.
func TestEnsureProject_GitignoreAlreadyHasJSlashEntry(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gi := filepath.Join(dir, ".gitignore")
	original := "  .j/  \nbuild/\n"
	if err := os.WriteFile(gi, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	got, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != original {
		t.Fatalf("gitignore changed: got %q, want %q", string(got), original)
	}
}

// TestEnsureProject_NoGitignoreLeavesNothingBehind pins the rule that
// EnsureProject does NOT manufacture a .gitignore for users who
// haven't opted into one.
func TestEnsureProject_NoGitignoreLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf(".gitignore should not have been created: err=%v", err)
	}
}

// TestEnsureProject_GitignoreReadFails turns the .gitignore path into
// a directory so os.ReadFile returns a non-NotExist error, exercising
// the read-error branch in ensureGitignoreEntry.
func TestEnsureProject_GitignoreReadFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir(filepath.Join(dir, ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProject(); err == nil {
		t.Fatal("expected error when .gitignore is a directory")
	} else if !strings.Contains(err.Error(), "read") {
		t.Fatalf("err = %v, want read failure", err)
	}
}

// TestEnsureProject_GitignoreAppendFails marks the existing
// .gitignore read-only so OpenFile with O_APPEND|O_WRONLY returns
// EACCES, exercising the append-error branch.
func TestEnsureProject_GitignoreAppendFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file-mode semantics required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode permissions")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	gi := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gi, []byte("foo\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(gi, 0o600) })
	if err := EnsureProject(); err == nil {
		t.Fatal("expected error writing to read-only .gitignore")
	} else if !strings.Contains(err.Error(), "write") {
		t.Fatalf("err = %v, want write failure", err)
	}
}

// TestEnsureProject_TouchSettingsFails forces the touchBoltFile error
// branch by parking a directory at .j/settings so bolt.Open cannot
// produce a regular file there.
func TestEnsureProject_TouchSettingsFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jdir := filepath.Join(dir, ".j")
	if err := os.MkdirAll(filepath.Join(jdir, "settings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProject(); err == nil {
		t.Fatal("expected touchBoltFile to fail")
	}
}


// TestEnsureProject_MkdirJDirFails forces the first os.MkdirAll error
// by parking a regular file at the .j path.
func TestEnsureProject_MkdirJDirFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, ".j"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProject(); err == nil {
		t.Fatal("expected mkdir error for .j path occupied by a file")
	}
}

// TestEnsureProject_PropagatesCwdError exercises the DefaultDir
// propagation branch by removing cwd out from under the helper.
func TestEnsureProject_PropagatesCwdError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cwd cannot be removed while in use on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root may bypass relevant FS errors")
	}
	parent := t.TempDir()
	gone := filepath.Join(parent, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	t.Setenv("PWD", "")
	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := DefaultDir(); err == nil {
		t.Skip("os.Getwd unexpectedly succeeded")
	}
	if err := EnsureProject(); err == nil {
		t.Fatal("EnsureProject should propagate DefaultDir error")
	}
}

// TestProjectInitialized_FullLayoutIsTrue pins the happy path.
func TestProjectInitialized_FullLayoutIsTrue(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	ok, err := ProjectInitialized()
	if err != nil {
		t.Fatalf("ProjectInitialized: %v", err)
	}
	if !ok {
		t.Fatal("expected initialized")
	}
}

// TestProjectInitialized_FreshDirIsFalse asserts the empty-cwd case.
func TestProjectInitialized_FreshDirIsFalse(t *testing.T) {
	t.Chdir(t.TempDir())
	ok, err := ProjectInitialized()
	if err != nil {
		t.Fatalf("ProjectInitialized: %v", err)
	}
	if ok {
		t.Fatal("expected not initialized on a fresh dir")
	}
}

// TestProjectInitialized_MissingArtifacts walks every individual
// missing-artifact case so the four "false" branches are covered.
func TestProjectInitialized_MissingArtifacts(t *testing.T) {
	cases := []struct {
		name   string
		remove string
	}{
		{"settings", "settings"},
		{"tasksDir", filepath.Join(tasksDirName, "")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			if err := EnsureProject(); err != nil {
				t.Fatalf("EnsureProject: %v", err)
			}
			target := filepath.Join(dir, ".j", c.remove)
			if err := os.RemoveAll(target); err != nil {
				t.Fatalf("RemoveAll: %v", err)
			}
			ok, err := ProjectInitialized()
			if err != nil {
				t.Fatalf("ProjectInitialized: %v", err)
			}
			if ok {
				t.Fatalf("expected not initialized after removing %s", c.name)
			}
		})
	}
}

// TestProjectInitialized_WrongTypeArtifacts pins the "exists with the
// wrong kind" branch (e.g. settings is a directory or tasks is a file).
func TestProjectInitialized_WrongTypeArtifacts(t *testing.T) {
	t.Run("settings_is_dir", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		if err := os.MkdirAll(filepath.Join(dir, ".j", "settings"), 0o755); err != nil {
			t.Fatal(err)
		}
		ok, err := ProjectInitialized()
		if err != nil {
			t.Fatalf("ProjectInitialized: %v", err)
		}
		if ok {
			t.Fatal("expected not initialized when settings is a directory")
		}
	})
	t.Run("tasks_is_file", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		jdir := filepath.Join(dir, ".j")
		if err := os.MkdirAll(jdir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(jdir, tasksDirName), []byte("legacy"), 0o600); err != nil {
			t.Fatal(err)
		}
		ok, err := ProjectInitialized()
		if err != nil {
			t.Fatalf("ProjectInitialized: %v", err)
		}
		if ok {
			t.Fatal("expected not initialized when tasks is a regular file")
		}
	})
}

// TestProjectInitialized_StatError forces a non-NotExist stat error
// (read-protected .j) so the propagation path is covered.
func TestProjectInitialized_StatError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file-mode semantics required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode permissions")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	jdir := filepath.Join(dir, ".j")
	if err := os.Chmod(jdir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(jdir, 0o755) })
	if _, err := ProjectInitialized(); err == nil {
		t.Fatal("expected stat error to propagate")
	}
}

// TestProjectInitialized_PropagatesCwdError covers the DefaultDir
// failure path.
func TestProjectInitialized_PropagatesCwdError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cwd cannot be removed while in use on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root may bypass relevant FS errors")
	}
	parent := t.TempDir()
	gone := filepath.Join(parent, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	t.Setenv("PWD", "")
	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := DefaultDir(); err == nil {
		t.Skip("os.Getwd unexpectedly succeeded")
	}
	if _, err := ProjectInitialized(); err == nil {
		t.Fatal("ProjectInitialized should propagate DefaultDir error")
	}
}
// TestTouchBoltFile_BoltOpenFails exercises the bolt.Open failure
// branch of the unexported touchBoltFile helper by pointing it at a
// path whose parent directory does not exist. EnsureProject never
// hits this case (it pre-creates parents) so we drive it directly.
func TestTouchBoltFile_BoltOpenFails(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no", "such", "parent", "settings")
	if err := touchBoltFile(missing); err == nil {
		t.Fatal("expected bolt.Open to fail when parent does not exist")
	} else if !strings.Contains(err.Error(), "open") {
		t.Fatalf("err = %v, want open failure", err)
	}
}

// TestTouchBoltFile_StatNonENOENT exercises the stat-error branch
// (errno other than ENOENT) by chmod'ing the parent directory to
// 0o000 so stat on the inner path returns EACCES rather than NotExist.
func TestTouchBoltFile_StatNonENOENT(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file-mode semantics required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode permissions")
	}
	parent := filepath.Join(t.TempDir(), "locked")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "settings")
	if err := os.WriteFile(target, []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	if err := touchBoltFile(target); err == nil {
		t.Fatal("expected stat error to propagate")
	}
}

// TestEnsureProject_MkdirTasksDirFails forces the os.MkdirAll error
// branch for the tasks subdirectory by making .j read-only after it
// exists but before EnsureProject runs. The existing .j dir survives
// the no-op MkdirAll for the parent, but the child mkdir fails.
func TestEnsureProject_MkdirTasksDirFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file-mode semantics required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode permissions")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	jdir := filepath.Join(dir, ".j")
	if err := os.MkdirAll(jdir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(jdir, 0o755) })
	if err := EnsureProject(); err == nil {
		t.Fatal("expected mkdir to fail on read-only .j")
	}
}

// TestEnsureGitignoreEntry_NotJDirIsNoop pins the early-return branch
// in ensureGitignoreEntry: the helper does nothing for arbitrary
// paths whose base name is not ".j" so callers using a custom store
// path are left untouched.
func TestEnsureGitignoreEntry_NotJDirIsNoop(t *testing.T) {
	dir := t.TempDir()
	gi := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gi, []byte("foo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureGitignoreEntry(filepath.Join(dir, "custom-store")); err != nil {
		t.Fatalf("ensureGitignoreEntry: %v", err)
	}
	got, err := os.ReadFile(gi)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "foo\n" {
		t.Fatalf(".gitignore = %q, want untouched", got)
	}
}

// TestProjectName covers the happy path (basename of cwd).
func TestProjectName(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "myproj")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	got, err := ProjectName()
	if err != nil {
		t.Fatalf("ProjectName: %v", err)
	}
	if got != "myproj" {
		t.Fatalf("ProjectName = %q, want %q", got, "myproj")
	}
}
