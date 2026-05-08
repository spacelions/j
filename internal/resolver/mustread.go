package resolver

import (
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/store"
)

// KeyMustRead is the bbolt key under store.BucketProject that holds
// the `;`-separated must-read list. The storage form matches the
// `j settings` key: project.must_read.
const KeyMustRead = "must_read"

// MustRead opens the per-project settings store, reads the must-read
// value, and returns it parsed into a list of files.
//
// On any failure (store can't be opened, value can't be read) it
// returns nil + a wrapped error so callers can log via their existing
// stderr-warning pattern and pass nil through to the agent — the
// workflow must never block on a must-read lookup.
func MustRead() ([]string, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("resolver: default path: %w", err)
	}
	s, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("resolver: open store: %w", err)
	}
	defer func() { _ = s.Close() }()
	value, set, err := s.Get(store.BucketProject, KeyMustRead)
	if err != nil {
		return nil, fmt.Errorf("resolver: load must-read: %w", err)
	}
	if !set {
		return nil, nil
	}
	return ParseMustRead(value), nil
}

// ParseMustRead splits a must-read value into individual file entries.
// Separator is `;`; surrounding whitespace is trimmed and empty
// fragments are dropped. Case is preserved verbatim so e.g. AGENTS.md
// and agents.md remain distinct on case-sensitive filesystems. An
// empty or whitespace-only input returns nil.
func ParseMustRead(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
