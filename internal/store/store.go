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
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"
)

// BucketPlanner is the bucket used by `j plan` to record the
// most-recently-selected durable tool/model/prompt settings.
const BucketPlanner = "planner"

// BucketWorker is the bucket used by `j work` to record the
// most-recently-selected durable tool/model/prompt settings.
const BucketWorker = "worker"

// BucketVerifier is the bucket used by `j verify` to record the
// most-recently-selected durable tool/model/prompt settings,
// mirroring BucketPlanner / BucketWorker.
const BucketVerifier = "verifier"

// BucketProject holds project-wide settings that aren't tied to a
// single role (planner / worker / verifier). The first key under it is
// "must_read", a `;`-separated list of files every agent should read
// before starting.
const BucketProject = "project"

// KeyProjectAPIKey is the storage key (under BucketProject) for the
// project's Gemini API token. The bucket-and-key pair is the
// canonical source for the value masked by `j settings` output and
// referenced by every settings reader.
const KeyProjectAPIKey = "api_key"

// dirName is the on-disk folder that holds the settings DB. It lives
// under the current working directory so each project gets its own
// state.
const dirName = ".j"

// fileName is the bbolt file inside dirName.
const fileName = "settings"

// tasksDirName is the per-project tasks directory inside dirName.
// The full tasks-package contract (the per-task TOML rows, lifecycle
// helpers, and so on) lives at internal/store/task; the constant
// here is private and only used by EnsureProject + ProjectInitialized
// to lay down the directory shell. Callers that need the externally
// visible form should import internal/store/task and reference
// tasks.DirName instead.
const tasksDirName = "tasks"

// openTimeout bounds how long we'll wait for a file lock when opening
// the bolt DB. A short timeout keeps tests responsive and surfaces
// concurrent-access bugs quickly.
const openTimeout = 2 * time.Second

// KV is a single bucket entry, returned in sorted-by-key order from List.
type KV struct {
	Key   string
	Value string
}

// Store wraps a *bbolt.DB for the settings file at `<cwd>/.j/settings`.
// Construct one with Open and call Close when done. The zero value is
// not usable. Task-side persistence lives in the sibling
// `internal/store/task` package; this Store has no notion of tasks.
type Store struct {
	db *bolt.DB
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
	slices.SortFunc(out, func(a, b KV) int { return cmp.Compare(a.Key, b.Key) })
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
// bucket has no key/value entries. The whole walk happens inside a
// single View transaction so we never have to chain a List call per
// bucket; the previous two-stage implementation surfaced a
// near-unreachable "list one bucket failed" branch that could not be
// driven from a test without racing the DB close.
func (s *Store) IsEmpty() (bool, error) {
	empty := true
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(_ []byte, b *bolt.Bucket) error {
			if k, _ := b.Cursor().First(); k != nil {
				empty = false
			}
			return nil
		})
	})
	if err != nil {
		return false, err
	}
	return empty, nil
}
