package linear

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spacelions/j/internal/store"
)

// LoadAPIKey reads the stored Linear API key from the per-project
// settings store. A missing settings file or a missing entry surface
// as ("", nil) so callers can branch on "no key yet" without an
// error-type check; only genuine bbolt failures surface verbatim.
func LoadAPIKey() (string, error) {
	return loadKey(store.KeyLinearAPIKey)
}

// SaveAPIKey persists token under linear.apiKey. The Linear bucket
// is created on demand so the helper works on a fresh project. The
// caller is expected to have run preflight (the cli's
// PersistentPreRunE) so the parent .j layout already exists.
func SaveAPIKey(token string) error {
	return saveKey(store.KeyLinearAPIKey, token)
}

// LoadProject reads the stored default Linear project id. Same
// missing-vs-error semantics as LoadAPIKey.
func LoadProject() (string, error) {
	return loadKey(store.KeyLinearProject)
}

// SaveProject persists id under linear.project. Same on-demand
// bucket creation as SaveAPIKey.
func SaveProject(id string) error {
	return saveKey(store.KeyLinearProject, id)
}

func loadKey(key string) (string, error) {
	path := store.DefaultPath()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("linear: stat %q: %w", path, err)
	}
	s, err := store.Open(path)
	if err != nil {
		return "", fmt.Errorf("linear: open settings: %w", err)
	}
	defer func() { _ = s.Close() }()
	v, _, _ := s.Get(store.BucketLinear, key)
	return v, nil
}

func saveKey(key, value string) error {
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		return fmt.Errorf("linear: open settings: %w", err)
	}
	defer func() { _ = s.Close() }()
	_ = s.EnsureBucket(store.BucketLinear)
	_ = s.Put(store.BucketLinear, key, value)
	return nil
}
