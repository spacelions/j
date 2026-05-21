package tasks

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to path via a sibling `.tmp-*` file
// followed by os.Rename, so a reader either sees the old contents or
// the fully-written new contents — never a partial write. The temp
// file is created in the same directory as the target so the rename
// stays on a single filesystem (cross-fs renames fail). On any error
// the temp file is best-effort removed so we don't litter.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("store: create temp %q: %w", path, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	_, _ = tmp.Write(data)
	_ = tmp.Chmod(perm)
	_ = tmp.Close()
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("store: rename %q: %w", path, err)
	}
	return nil
}
