package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFileAtomic_HappyPath verifies a successful round-trip.
func TestWriteFileAtomic_HappyPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.toml")
	if err := writeFileAtomic(target, []byte("x = 1"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "x = 1" {
		t.Fatalf("data = %q, want x = 1", data)
	}
}

// TestWriteFileAtomic_CreateTempError makes the target directory read-only so
// os.CreateTemp fails, covering the createTemp error branch.
func TestWriteFileAtomic_CreateTempError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	err := writeFileAtomic(filepath.Join(dir, "out.toml"), []byte("x = 1"), 0o644)
	if err == nil {
		t.Fatal("writeFileAtomic should fail when dir is not writable")
	}
}

// TestWriteFileAtomic_RenameError makes the target path a directory so
// os.Rename(tmpFile, dir) fails with EISDIR, exercising the rename-error
// and cleanup branches.
func TestWriteFileAtomic_RenameError(t *testing.T) {
	dir := t.TempDir()
	// target is a directory: rename a regular temp file over it fails.
	target := filepath.Join(dir, "out.toml")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	err := writeFileAtomic(target, []byte("x = 2"), 0o644)
	if err == nil {
		t.Fatal("writeFileAtomic should fail when rename target is a dir")
	}
}
