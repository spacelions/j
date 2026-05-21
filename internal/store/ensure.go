package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	bolt "go.etcd.io/bbolt"
)

// EnsureProject creates the per-project state layout under <cwd>/:
//   - .j/                   (0o755)
//   - .j/tasks/             (0o755)
//   - .j/settings           (empty bbolt file with no buckets)
//
// It also appends `.j` to <cwd>/.gitignore when one already exists
// and does not yet carry the entry. The helper is idempotent: a
// re-run on a fully-initialized layout creates nothing new but is
// not an error.
//
// Tasks are stored as per-task TOML files at
// `<cwd>/.j/tasks/<id>/task.toml` — there is no central tasks DB
// file to pre-create here; per-task directories are created on
// demand by EnsureTaskDir.
//
// EnsureProject is the only write-side helper in this package: every
// other Open / EnsureTaskDir call assumes the layout exists and
// surfaces a wrapped error otherwise. `j init` and the pre-flight
// confirm path are the sole callers.
func EnsureProject() error {
	jDir := DefaultDir()
	if err := os.MkdirAll(jDir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir %q: %w", jDir, err)
	}
	tasksDir := filepath.Join(jDir, tasksDirName)
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir %q: %w", tasksDir, err)
	}
	if err := touchBoltFile(filepath.Join(jDir, fileName)); err != nil {
		return err
	}
	return ensureGitignoreEntry(jDir)
}

// touchBoltFile opens path with bolt.Open and immediately closes the
// resulting handle when the file does not yet exist. The side effect
// is the same as the legacy lazy path: a valid (empty) bbolt file
// appears at path. Buckets are NOT pre-created so callers can rely
// on CreateBucketIfNotExists to mint them on first write. When the
// file already exists EnsureProject treats it as user data and skips
// the open call so an unrelated (non-bbolt) file at the same path
// doesn't trigger a misleading "invalid database" error.
func touchBoltFile(path string) error {
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return fmt.Errorf("store: %q is a directory", path)
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("store: stat %q: %w", path, err)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: openTimeout})
	if err != nil {
		return fmt.Errorf("store: open %q: %w", path, err)
	}
	_ = db.Close()
	return nil
}

// ProjectInitialized reports whether the three artifacts written by
// EnsureProject (the .j directory, the settings bbolt file, the
// tasks subdirectory) are all present in the current working
// directory. It returns (true, nil) only when every artifact exists
// with the expected type; missing artifacts yield (false, nil) so
// callers can distinguish "needs init" from "stat error". Per-task
// directories under `.j/tasks/<id>/` are not part of the
// initialisation contract — they are created on demand by
// EnsureTaskDir as tasks are minted.
func ProjectInitialized() (bool, error) {
	jDir := DefaultDir()
	checks := []struct {
		path  string
		isDir bool
	}{
		{jDir, true},
		{filepath.Join(jDir, fileName), false},
		{filepath.Join(jDir, tasksDirName), true},
	}
	for _, c := range checks {
		ok, err := pathHasKind(c.path, c.isDir)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// ensureGitignoreEntry appends ".j" to an existing <parent>/.gitignore
// when the just-created directory is the per-project ".j" folder. The
// helper is intentionally narrow: it does nothing if jDir is not named
// ".j" (so arbitrary custom store paths are left untouched), it does
// nothing if .gitignore is absent (we don't manufacture one for users
// who haven't opted into git), and it returns a wrapped error only
// when an existing .gitignore cannot be read or appended to.
func ensureGitignoreEntry(jDir string) error {
	if filepath.Base(jDir) != dirName {
		return nil
	}
	gitignorePath := filepath.Join(filepath.Dir(jDir), ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("store: read %q: %w", gitignorePath, err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == dirName || trimmed == dirName+"/" {
			return nil
		}
	}
	var prefix string
	if len(data) > 0 && data[len(data)-1] != '\n' {
		prefix = "\n"
	}
	//nolint:gocritic // intentional new slice; data preserved to check last byte
	updated := append(data, []byte(prefix+dirName+"\n")...)
	// os.WriteFile preserves existing perms on update.
	if err := os.WriteFile(gitignorePath, updated, 0o600); err != nil {
		return fmt.Errorf("store: write %q: %w", gitignorePath, err)
	}
	return nil
}
