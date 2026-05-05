package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

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

// TestWriteFileAtomic_RenameError writes the temp file successfully but makes
// the target directory read-only before the rename so os.Rename fails.
func TestWriteFileAtomic_RenameError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "out.toml")

	// Pre-create the file so its dir is writable during temp-file creation.
	// Then lock the directory after the write but before the rename by
	// wrapping in a subdir that we can chmod.
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	target = filepath.Join(sub, "out.toml")

	// First write succeeds (sub is writable).
	if err := writeFileAtomic(target, []byte("x = 1"), 0o644); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Now lock sub so rename will fail on the second write.
	if err := os.Chmod(sub, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })

	err := writeFileAtomic(target, []byte("x = 2"), 0o644)
	if err == nil {
		t.Fatal("writeFileAtomic should fail when rename is blocked")
	}
}
