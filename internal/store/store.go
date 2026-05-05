// Package store is a tiny bbolt-backed key/value store used by the j
// CLI to persist user-facing settings (which planner tool/model was
// last used, etc.). It deliberately does NOT define an interface: per
// AGENTS.md ("no seams, use allowlist") callers depend on the concrete
// *Store and tests drive isolation by chdir'ing into a temp dir.
//
// Write-side responsibility for the on-disk layout is concentrated in
// EnsureProject (called by `j init` and by the pre-flight confirm
// path). Every other helper here is read/write only and assumes the
// layout is already present; callers that need creation must invoke
// EnsureProject first.
package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

// BucketPlanner is the bucket used by `j plan` to record the
// most-recently-selected tool/model/interactive flag.
const BucketPlanner = "planner"

// BucketWorker is the bucket used by `j work` to record the
// most-recently-selected tool/model/interactive flag.
const BucketWorker = "worker"

// BucketVerifier is the bucket used by `j verify` to record the
// most-recently-selected tool/model/interactive flag, mirroring
// BucketPlanner / BucketWorker.
const BucketVerifier = "verifier"

// BucketProject holds project-wide settings that aren't tied to a
// single role (planner / worker / verifier). The first key under it is
// "must_read", a `;`-separated list of files every agent should read
// before starting.
const BucketProject = "project"

// BucketLinear holds Linear-specific settings (the personal API key
// and the default Linear project id). It is created on first write
// from `j settings set linear.…` or from the source picker's link
// flow; absent until the user authenticates once.
const BucketLinear = "linear"

// KeyLinearAPIKey is the storage key (under BucketLinear) for the
// personal Linear API token (`lin_api_…`). User-typed forms
// `linear.api_key` and `linear.api-key` both round-trip to this key
// via the settings storageKey helper. The on-disk form matches
// project.api_key so a single grep finds every secret in the store.
const KeyLinearAPIKey = "api_key"

// KeyLinearProject is the storage key (under BucketLinear) for the
// default Linear project id captured during the link flow. Optional;
// surfaces in `j settings` once set.
const KeyLinearProject = "project"

// dirName is the on-disk folder that holds the settings DB. It lives
// under the current working directory so each project gets its own
// state.
const dirName = ".j"

// fileName is the bbolt file inside dirName.
const fileName = "settings"

// TasksDirName is the per-project tasks directory inside dirName. The
// directory holds both the bbolt metadata file (TasksDBName) and one
// subdirectory per task (`<id>/`) with `requirements.md` and
// `plan.md`.
const TasksDirName = "tasks"

// TasksDBName is the bbolt filename inside <cwd>/.j/<TasksDirName>/.
// It carries task metadata only; the body markdown lives in the
// per-task subdirectories.
const TasksDBName = "list.db"

// PlanFileName is the filename of the plan markdown stored under
// <cwd>/.j/tasks/<id>/. j plan writes it; j work reads it.
const PlanFileName = "plan.md"

// RequirementsFileName is the filename of the requirements markdown
// stored under <cwd>/.j/tasks/<id>/. j plan writes it; j work and
// j tasks summary derivation read it.
const RequirementsFileName = "requirements.md"

// openTimeout bounds how long we'll wait for a file lock when opening
// the bolt DB. A short timeout keeps tests responsive and surfaces
// concurrent-access bugs quickly.
const openTimeout = 2 * time.Second

// KV is a single bucket entry, returned in sorted-by-key order from List.
type KV struct {
	Key   string
	Value string
}

// Store wraps a *bbolt.DB. Construct one with Open and call Close when
// done. The zero value is not usable.
type Store struct {
	db *bolt.DB
}

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
// from the same source. A non-nil error only surfaces when os.Getwd
// itself fails (e.g. the current directory was removed while the
// process is running); the caller decides whether to treat that as
// fatal or silently fall back to an empty project slug (fillWorktree
// does the latter so a cosmetic worktree label never blocks `j work`).
func ProjectName() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("store: resolve cwd: %w", err)
	}
	return filepath.Base(cwd), nil
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

// DefaultTasksDir returns the absolute path to the per-project tasks
// directory (`<cwd>/.j/tasks`). The directory holds the bbolt metadata
// file (DefaultTasksDBPath) plus one subdirectory per task.
func DefaultTasksDir() (string, error) {
	dir, err := DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, TasksDirName), nil
}

// DefaultTasksDBPath returns the absolute path to the bbolt task
// metadata file at `<cwd>/.j/tasks/list.db`.
func DefaultTasksDBPath() (string, error) {
	tasksDir, err := DefaultTasksDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(tasksDir, TasksDBName), nil
}

// EnsureProject creates the per-project state layout under <cwd>/:
//   - .j/                   (0o755)
//   - .j/tasks/             (0o755)
//   - .j/settings           (empty bbolt file with no buckets)
//   - .j/tasks/list.db      (empty bbolt file with no buckets)
//
// It also appends `.j` to <cwd>/.gitignore when one already exists
// and does not yet carry the entry. The helper is idempotent: a
// re-run on a fully-initialized layout creates nothing new but is
// not an error.
//
// EnsureProject is the only write-side helper in this package: every
// other Open / EnsureTaskDir call assumes the layout exists and
// surfaces a wrapped error otherwise. `j init` and the pre-flight
// confirm path are the sole callers.
func EnsureProject() error {
	jDir, err := DefaultDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(jDir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir %q: %w", jDir, err)
	}
	tasksDir := filepath.Join(jDir, TasksDirName)
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir %q: %w", tasksDir, err)
	}
	if err := touchBoltFile(filepath.Join(jDir, fileName)); err != nil {
		return err
	}
	if err := touchBoltFile(filepath.Join(tasksDir, TasksDBName)); err != nil {
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
	if err := db.Close(); err != nil {
		return fmt.Errorf("store: close %q: %w", path, err)
	}
	return nil
}

// ProjectInitialized reports whether the four artifacts written by
// EnsureProject (the two directories and the two bbolt files) are
// all present in the current working directory. It returns
// (true, nil) only when every artifact exists with the expected
// type; missing artifacts yield (false, nil) so callers can
// distinguish "needs init" from "stat error".
func ProjectInitialized() (bool, error) {
	jDir, err := DefaultDir()
	if err != nil {
		return false, err
	}
	checks := []struct {
		path  string
		isDir bool
	}{
		{jDir, true},
		{filepath.Join(jDir, fileName), false},
		{filepath.Join(jDir, TasksDirName), true},
		{filepath.Join(jDir, TasksDirName, TasksDBName), false},
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

// EnsureTaskDir creates `<cwd>/.j/tasks/<id>/` (with mkdir -p) and
// returns its absolute path. The parent `.j/tasks/` directory must
// already exist (created by `j init` via EnsureProject); a missing
// parent surfaces a wrapped fs.ErrNotExist so callers can prompt the
// user to run init.
func EnsureTaskDir(id string) (string, error) {
	if id == "" {
		return "", errors.New("store: empty task id")
	}
	tasksDir, err := DefaultTasksDir()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(tasksDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("store: %q missing; run `j init`: %w", tasksDir, err)
		}
		return "", fmt.Errorf("store: stat %q: %w", tasksDir, err)
	}
	taskDir := filepath.Join(tasksDir, id)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return "", fmt.Errorf("store: mkdir %q: %w", taskDir, err)
	}
	return taskDir, nil
}

// RemoveTaskDir removes `<cwd>/.j/tasks/<id>/` and every artifact
// inside it. The parent `.j/tasks/` directory must already exist
// (created by `j init` via EnsureProject); a missing parent surfaces
// a wrapped fs.ErrNotExist so callers can prompt the user to run
// init (mirroring EnsureTaskDir). The helper is idempotent: a
// missing per-task directory is treated as a no-op because
// os.RemoveAll returns nil when the target is absent.
func RemoveTaskDir(id string) error {
	if id == "" {
		return errors.New("store: empty task id")
	}
	tasksDir, err := DefaultTasksDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(tasksDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("store: %q missing; run `j init`: %w", tasksDir, err)
		}
		return fmt.Errorf("store: stat %q: %w", tasksDir, err)
	}
	taskDir := filepath.Join(tasksDir, id)
	if err := os.RemoveAll(taskDir); err != nil {
		return fmt.Errorf("store: remove %q: %w", taskDir, err)
	}
	return nil
}

// Open opens the bolt database at path. The parent directory and the
// file itself must already exist (EnsureProject is the sole creator);
// a missing path yields a wrapped fs.ErrNotExist so callers can
// prompt the user to run `j init`. Open does NOT pre-create any
// buckets; callers should invoke EnsureBucket as needed.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("store: empty path")
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: openTimeout})
	if err != nil {
		return nil, fmt.Errorf("store: open %q: %w", path, err)
	}
	return &Store{db: db}, nil
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
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == dirName || trimmed == dirName+"/" {
			return nil
		}
	}
	var prefix string
	if len(data) > 0 && data[len(data)-1] != '\n' {
		prefix = "\n"
	}
	updated := append(data, []byte(prefix+dirName+"\n")...)
	// os.WriteFile preserves the existing file's mode (perm is only
	// applied on create), so we keep whatever permissions the user had.
	if err := os.WriteFile(gitignorePath, updated, 0o600); err != nil {
		return fmt.Errorf("store: write %q: %w", gitignorePath, err)
	}
	return nil
}

// Close releases the underlying bolt DB.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// EnsureBucket creates the bucket if it does not already exist. Calling
// it on an existing bucket is a no-op.
func (s *Store) EnsureBucket(name string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(name))
		return err
	})
}

// Put writes value under key in bucket. The bucket is created if
// missing so callers don't need to call EnsureBucket first.
func (s *Store) Put(bucket, key, value string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		return b.Put([]byte(key), []byte(value))
	})
}

// Get returns the value stored under key in bucket. The boolean is
// false when the bucket or key does not exist; in that case the error
// is nil. An empty stored value is reported as ("", true, nil).
func (s *Store) Get(bucket, key string) (string, bool, error) {
	var (
		val   string
		found bool
	)
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		v := b.Get([]byte(key))
		if v == nil {
			return nil
		}
		val = string(v)
		found = true
		return nil
	})
	if err != nil {
		return "", false, err
	}
	return val, found, nil
}

// List returns every key/value pair in bucket, sorted by key. A
// missing bucket yields an empty slice and no error so callers can
// treat "no settings" identically to "no bucket yet".
func (s *Store) List(bucket string) ([]KV, error) {
	var out []KV
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			out = append(out, KV{Key: string(k), Value: string(v)})
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// ListBuckets returns every top-level bucket name, sorted.
func (s *Store) ListBuckets() ([]string, error) {
	var names []string
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, _ *bolt.Bucket) error {
			names = append(names, string(name))
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// Delete removes key from bucket. Missing bucket or key is a no-op
// with a nil error. Other failures are returned wrapped.
func (s *Store) Delete(bucket, key string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		if b.Get([]byte(key)) == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}

// DeleteBucket removes the named bucket and every key inside it. A
// missing bucket is a no-op with a nil error, mirroring Delete's
// missing-key semantics so callers can ask for a wipe without
// pre-checking existence. Other failures (closed DB, etc.) propagate.
func (s *Store) DeleteBucket(name string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte(name)) == nil {
			return nil
		}
		return tx.DeleteBucket([]byte(name))
	})
}

// IsEmpty reports whether the database has no buckets or every
// bucket has no key/value entries.
func (s *Store) IsEmpty() (bool, error) {
	buckets, err := s.ListBuckets()
	if err != nil {
		return false, err
	}
	if len(buckets) == 0 {
		return true, nil
	}
	for _, name := range buckets {
		entries, err := s.List(name)
		if err != nil {
			return false, err
		}
		if len(entries) > 0 {
			return false, nil
		}
	}
	return true, nil
}
