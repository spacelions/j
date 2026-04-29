package store

import (
	"errors"
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

func TestOpen_CreatesParentDir(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "child", "settings")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("parent dir not created: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("db file not created: %v", err)
	}
}

// TestOpen_MkdirFails forces MkdirAll to fail by pointing the parent at
// a regular file (which cannot become a directory).
func TestOpen_MkdirFails(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "nested", "settings")
	if _, err := Open(path); err == nil {
		t.Fatal("expected mkdir error")
	}
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

func openInTemp(t *testing.T) *Store {
	t.Helper()
	t.Chdir(t.TempDir())
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

func TestOpen_AppendsToExistingGitignore(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gi := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gi, []byte("node_modules/\n"), 0o600); err != nil {
		t.Fatal(err)
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

	got, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "node_modules/\n.j\n"
	if string(got) != want {
		t.Fatalf("gitignore = %q, want %q", string(got), want)
	}
}

func TestOpen_GitignoreWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gi := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gi, []byte("node_modules/"), 0o600); err != nil {
		t.Fatal(err)
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

	got, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "node_modules/\n.j\n"
	if string(got) != want {
		t.Fatalf("gitignore = %q, want %q", string(got), want)
	}
}

func TestOpen_GitignoreAlreadyHasJEntry(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gi := filepath.Join(dir, ".gitignore")
	original := "node_modules/\n.j\nbuild/\n"
	if err := os.WriteFile(gi, []byte(original), 0o600); err != nil {
		t.Fatal(err)
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

	got, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != original {
		t.Fatalf("gitignore changed: got %q, want %q", string(got), original)
	}
}

func TestOpen_GitignoreAlreadyHasJSlashEntry(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gi := filepath.Join(dir, ".gitignore")
	original := "  .j/  \nbuild/\n"
	if err := os.WriteFile(gi, []byte(original), 0o600); err != nil {
		t.Fatal(err)
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

	got, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != original {
		t.Fatalf("gitignore changed: got %q, want %q", string(got), original)
	}
}

func TestOpen_NoGitignoreLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf(".gitignore should not have been created: err=%v", err)
	}
}

// TestOpen_NonDotJCustomPathDoesNotTouchGitignore confirms the helper
// is scoped to the default `.j` directory: a custom path whose parent
// is not named `.j` must leave any neighbouring `.gitignore` alone.
func TestOpen_NonDotJCustomPathDoesNotTouchGitignore(t *testing.T) {
	dir := t.TempDir()
	gi := filepath.Join(dir, ".gitignore")
	original := "node_modules/\n"
	if err := os.WriteFile(gi, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "custom", "settings")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	got, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != original {
		t.Fatalf("gitignore changed: got %q, want %q", string(got), original)
	}
}

// TestOpen_GitignoreReadFails turns the .gitignore path into a
// directory so os.ReadFile returns a non-NotExist error, exercising
// the read-error branch in ensureGitignoreEntry.
func TestOpen_GitignoreReadFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir(filepath.Join(dir, ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("expected error when .gitignore is a directory")
	} else if !strings.Contains(err.Error(), "read") {
		t.Fatalf("err = %v, want read failure", err)
	}
}

// TestOpen_GitignoreAppendFails marks the existing .gitignore
// read-only so OpenFile with O_APPEND|O_WRONLY returns EACCES,
// exercising the append-error branch.
func TestOpen_GitignoreAppendFails(t *testing.T) {
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

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("expected error writing to read-only .gitignore")
	} else if !strings.Contains(err.Error(), "write") {
		t.Fatalf("err = %v, want write failure", err)
	}
}

// TestStore_OperationsAfterClose drives the error paths in Put/Get/List/
// EnsureBucket/ListBuckets by invoking them on a closed DB.
func TestStore_OperationsAfterClose(t *testing.T) {
	t.Chdir(t.TempDir())
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

// TestDefaultTasksDir_RootedInCwd pins the new tasks-folder layout:
// <cwd>/.j/tasks/ with the bbolt file at index.db inside it.
func TestDefaultTasksDir_RootedInCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gotDir, err := DefaultTasksDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	if filepath.Base(gotDir) != TasksDirName {
		t.Fatalf("DefaultTasksDir base = %q, want %q", filepath.Base(gotDir), TasksDirName)
	}
	if filepath.Base(filepath.Dir(gotDir)) != ".j" {
		t.Fatalf("DefaultTasksDir parent = %q, want .j", filepath.Base(filepath.Dir(gotDir)))
	}
	gotDB, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	if filepath.Base(gotDB) != TasksDBName {
		t.Fatalf("DefaultTasksDBPath base = %q, want %q", filepath.Base(gotDB), TasksDBName)
	}
	if filepath.Dir(gotDB) != gotDir {
		t.Fatalf("DefaultTasksDBPath dir = %q, want %q", filepath.Dir(gotDB), gotDir)
	}
}

func TestDefaultTasksDir_PropagatesCwdError(t *testing.T) {
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
	if _, err := DefaultTasksDir(); err == nil {
		t.Fatal("DefaultTasksDir should propagate DefaultDir error")
	}
	if _, err := DefaultTasksDBPath(); err == nil {
		t.Fatal("DefaultTasksDBPath should propagate DefaultDir error")
	}
}

// TestEnsureTaskDir_CreatesNested verifies the per-task directory is
// created with mkdir -p semantics inside .j/tasks/, that the parent
// .j/ exists, and that calling it again is a no-op.
func TestEnsureTaskDir_CreatesNested(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	taskDir, err := EnsureTaskDir("abc")
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if filepath.Base(taskDir) != "abc" {
		t.Fatalf("EnsureTaskDir base = %q", filepath.Base(taskDir))
	}
	info, err := os.Stat(taskDir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("EnsureTaskDir did not create a directory")
	}
	jdir := filepath.Join(dir, ".j")
	if info, err := os.Stat(jdir); err != nil || !info.IsDir() {
		t.Fatalf(".j parent should exist, got err=%v", err)
	}
	tasksDir := filepath.Join(jdir, TasksDirName)
	if info, err := os.Stat(tasksDir); err != nil || !info.IsDir() {
		t.Fatalf(".j/tasks should exist, got err=%v", err)
	}
	// Idempotent.
	if _, err := EnsureTaskDir("abc"); err != nil {
		t.Fatalf("second EnsureTaskDir: %v", err)
	}
}

// TestEnsureTaskDir_AppendsGitignoreEntry pins that EnsureTaskDir
// invokes the .gitignore allowlist when one already exists in the
// repo root.
func TestEnsureTaskDir_AppendsGitignoreEntry(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gi := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gi, []byte("node_modules/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureTaskDir("abc"); err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	got, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(got), ".j") {
		t.Fatalf("gitignore = %q, want .j entry", string(got))
	}
}

func TestEnsureTaskDir_RejectsEmptyID(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := EnsureTaskDir(""); err == nil {
		t.Fatal("expected error for empty id")
	}
}

// TestEnsureTaskDir_ParentMkdirReadOnly covers the os.MkdirAll error
// for the per-task subdirectory when its parent (.j/tasks) is
// read-only. The helper must surface a wrapped mkdir error.
func TestEnsureTaskDir_ParentMkdirReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file-mode semantics required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode permissions")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if _, err := EnsureTaskDir("seed"); err != nil {
		t.Fatalf("seed EnsureTaskDir: %v", err)
	}
	tasksDir := filepath.Join(dir, ".j", TasksDirName)
	if err := os.Chmod(tasksDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tasksDir, 0o755) })

	if _, err := EnsureTaskDir("locked"); err == nil {
		t.Fatal("expected mkdir error inside read-only parent")
	} else if !strings.Contains(err.Error(), "mkdir") {
		t.Fatalf("err = %v, want wrapped mkdir error", err)
	}
}

// TestEnsureTaskDir_TasksDirIsFile covers the ensureTasksDir branch
// where the *sibling* path is a regular file (legacy schema). It
// reuses the LegacyTasksFile assertion so the same fixture exercises
// both the public helper and the package-private branch.
func TestEnsureTaskDir_LegacyTasksFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jdir := filepath.Join(dir, ".j")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(jdir, "tasks")
	if err := os.WriteFile(legacy, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := EnsureTaskDir("abc")
	if !errors.Is(err, ErrLegacyTasksFile) {
		t.Fatalf("err = %v, want ErrLegacyTasksFile", err)
	}
}

// TestTaskFileNameConstants pins the package-level filename constants
// that callers use with filepath.Join(DefaultTasksDir(), id, name) to
// build per-task body paths. The values are part of the on-disk
// layout contract, so changing them requires a migration.
func TestTaskFileNameConstants(t *testing.T) {
	if PlanFileName != "plan.md" {
		t.Fatalf("PlanFileName = %q, want %q", PlanFileName, "plan.md")
	}
	if RequirementsFileName != "requirements.md" {
		t.Fatalf("RequirementsFileName = %q, want %q", RequirementsFileName, "requirements.md")
	}
}
