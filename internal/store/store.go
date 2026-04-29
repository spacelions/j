// Package store is a tiny bbolt-backed key/value store used by the j
// CLI to persist user-facing settings (which planner tool/model was
// last used, etc.). It deliberately does NOT define an interface: per
// AGENTS.md ("no seams, use allowlist") callers depend on the concrete
// *Store and tests drive isolation by chdir'ing into a temp dir.
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

// BucketCoder is the bucket used by `j work` to record the
// most-recently-selected tool/model/interactive flag.
const BucketCoder = "coder"

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
const TasksDBName = "index.db"

// PlanFileName is the filename of the plan markdown stored under
// <cwd>/.j/tasks/<id>/. j plan writes it; j work reads it.
const PlanFileName = "plan.md"

// RequirementsFileName is the filename of the requirements markdown
// stored under <cwd>/.j/tasks/<id>/. j plan writes it; j work and
// j tasks summary derivation read it.
const RequirementsFileName = "requirements.md"

// ErrLegacyTasksFile is returned by Open / OpenTaskLog when the path
// `<cwd>/.j/tasks` exists as a regular file rather than a directory.
// That layout corresponds to the previous bbolt-only schema; the new
// schema uses `<cwd>/.j/tasks/` as a directory and stores the bbolt
// file inside it as `index.db`. Callers should surface a clear "rename
// or remove the legacy file" message.
var ErrLegacyTasksFile = errors.New("store: found legacy .j/tasks regular file; rename or remove it before continuing")

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
// metadata file at `<cwd>/.j/tasks/index.db`.
func DefaultTasksDBPath() (string, error) {
	tasksDir, err := DefaultTasksDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(tasksDir, TasksDBName), nil
}

// EnsureTaskDir creates `<cwd>/.j/tasks/<id>/` (with mkdir -p) and
// returns its absolute path. It also creates the parent `.j/tasks/`
// directory and the `.j/.gitignore` entry, mirroring Open's setup.
// When `.j/tasks` exists as a regular file the helper returns
// ErrLegacyTasksFile so the cmd layer can surface a friendly message.
func EnsureTaskDir(id string) (string, error) {
	if id == "" {
		return "", errors.New("store: empty task id")
	}
	tasksDir, err := DefaultTasksDir()
	if err != nil {
		return "", err
	}
	if err := ensureTasksDir(tasksDir); err != nil {
		return "", err
	}
	taskDir := filepath.Join(tasksDir, id)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return "", fmt.Errorf("store: mkdir %q: %w", taskDir, err)
	}
	return taskDir, nil
}

// ensureTasksDir mkdirs `<cwd>/.j/tasks/`, gitignore-tags the parent
// `.j/`, and rejects pre-existing regular files at the tasks path
// (the legacy bbolt-only schema). It is idempotent.
func ensureTasksDir(tasksDir string) error {
	jDir := filepath.Dir(tasksDir)
	if err := os.MkdirAll(jDir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir %q: %w", jDir, err)
	}
	if err := ensureGitignoreEntry(jDir); err != nil {
		return err
	}
	if info, err := os.Stat(tasksDir); err == nil && !info.IsDir() {
		return ErrLegacyTasksFile
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("store: stat %q: %w", tasksDir, err)
	}
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir %q: %w", tasksDir, err)
	}
	return nil
}

// Open creates the parent directory (if missing) and opens the bolt
// database at path. It does NOT pre-create any buckets; callers should
// invoke EnsureBucket as needed.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("store: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("store: mkdir %q: %w", filepath.Dir(path), err)
	}
	if err := ensureGitignoreEntry(filepath.Dir(path)); err != nil {
		return nil, err
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
