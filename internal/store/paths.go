package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultDir returns the absolute path to the per-project settings
// directory (`<cwd>/.j`). It is exposed for callers that want to
// surface the location to the user without opening the DB.
func DefaultDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("store: resolve cwd: %w", err)
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("store: resolve cwd abs: %w", err)
	}
	return filepath.Join(abs, dirName), nil
}

// ProjectName returns the basename of the current working directory.
// It is the single rule used by WorktreeNameFor so every call site —
// `j work`, tests, any future caller — derives the project slug
// from the same source. A Getwd failure (e.g. the current directory
// was removed while the process is running) yields filepath.Base("")
// = "." which slugify normalises to ""; callers that build worktree
// names then naturally fall back to a task-only label rather than
// blocking on a cosmetic lookup.
func ProjectName() string {
	cwd, _ := os.Getwd()
	return filepath.Base(cwd)
}

// DefaultPath returns the absolute path to the default settings DB
// (`<cwd>/.j/settings`).
func DefaultPath() (string, error) {
	dir, err := DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// pathHasKind returns true when path exists as the requested kind
// (directory when isDir is true, regular file otherwise). A
// fs.ErrNotExist stat error yields (false, nil); any other stat
// error propagates.
func pathHasKind(path string, isDir bool) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("store: stat %q: %w", path, err)
	}
	if isDir {
		return info.IsDir(), nil
	}
	return !info.IsDir(), nil
}
