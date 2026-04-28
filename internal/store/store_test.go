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
}
