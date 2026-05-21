package tasks

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/store"
)

func TestOpenDefault_HappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	s := OpenDefault()
	if s == nil {
		t.Fatal("OpenDefault returned nil store")
	}
	if s.Dir() == "" {
		t.Fatal("Dir() returned empty path")
	}
}

func TestStore_Dir_ReturnsTasksDir(t *testing.T) {
	s := Open("/some/path")
	if s.Dir() != "/some/path" {
		t.Fatalf("Dir() = %q, want /some/path", s.Dir())
	}
}

func TestEnsureDir_HappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	taskDir, err := EnsureDir("test-id")
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if _, err := os.Stat(taskDir); err != nil {
		t.Fatalf("task dir not created: %v", err)
	}
	if filepath.Base(taskDir) != "test-id" {
		t.Fatalf("Dir base = %q, want test-id", filepath.Base(taskDir))
	}
}

func TestEnsureDir_EmptyID(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	_, err := EnsureDir("")
	if err == nil {
		t.Fatal("EnsureDir with empty id should error")
	}
}

func TestEnsureDir_MissingTasksDir(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := EnsureDir("x")
	if err == nil {
		t.Fatal("EnsureDir without a .j directory should error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestRemoveDir_HappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	taskDir, err := EnsureDir("remove-me")
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := RemoveDir("remove-me"); err != nil {
		t.Fatalf("RemoveDir: %v", err)
	}
	if _, err := os.Stat(taskDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("task dir still exists after RemoveDir: err = %v", err)
	}
}

func TestRemoveDir_EmptyID(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := RemoveDir(""); err == nil {
		t.Fatal("RemoveDir with empty id should error")
	}
}

func TestRemoveDir_MissingTasksDir(t *testing.T) {
	t.Chdir(t.TempDir())
	err := RemoveDir("x")
	if err == nil {
		t.Fatal("RemoveDir without a .j directory should error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

// TestEnsureDir_TasksDirIsFile replaces .j/tasks with a regular file so that
// os.Stat succeeds but MkdirAll fails, covering the mkdir error branch.
func TestEnsureDir_TasksDirIsFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	tasksDir := DefaultDir()
	if err := os.RemoveAll(tasksDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tasksDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := EnsureDir("x")
	if err == nil {
		t.Fatal("EnsureDir with tasks-as-file should error")
	}
}

// TestEnsureDir_TasksDirStatFails forces a non-NotExist stat error
// by chmod-zeroing the `.j` parent so stat on `.j/tasks` returns
// EACCES instead of ENOENT.
func TestEnsureDir_TasksDirStatFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode permissions")
	}
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	jDir := store.DefaultDir()
	if err := os.Chmod(jDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(jDir, 0o755) })
	_, err := EnsureDir("x")
	if err == nil {
		t.Fatal("EnsureDir should surface a stat EACCES error")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want non-NotExist", err)
	}
}

// TestRemoveDir_TasksDirStatFails mirrors EnsureDir's stat-error
// branch for the removal path.
func TestRemoveDir_TasksDirStatFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode permissions")
	}
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	jDir := store.DefaultDir()
	if err := os.Chmod(jDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(jDir, 0o755) })
	err := RemoveDir("x")
	if err == nil {
		t.Fatal("RemoveDir should surface a stat EACCES error")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want non-NotExist", err)
	}
}

// TestRemoveDir_RemoveAllFails chmod-zeroes the tasks parent after
// stat passes so os.RemoveAll cannot unlink the target.
func TestRemoveDir_RemoveAllFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode permissions")
	}
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	taskDir, err := EnsureDir("locked")
	if err != nil {
		t.Fatal(err)
	}
	// Create a sub-entry so RemoveAll has actual work that requires
	// write perms on the parent.
	if err := os.WriteFile(
		filepath.Join(taskDir, "x"), []byte("y"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	tasksDir := DefaultDir()
	if err := os.Chmod(tasksDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tasksDir, 0o755) })
	if err := RemoveDir("locked"); err == nil {
		t.Fatal("RemoveDir should fail when tasks dir is read-only")
	}
}

func TestClarificationFileExists(t *testing.T) {
	taskDir := t.TempDir()
	if ClarificationFileExists(taskDir) {
		t.Fatal("ClarificationFileExists = true before file exists")
	}

	path := filepath.Join(taskDir, ClarificationFileName)
	if err := os.WriteFile(path, []byte("question?"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ClarificationFileExists(taskDir) {
		t.Fatal("ClarificationFileExists = false after file exists")
	}
}
