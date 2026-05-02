// Package mustread holds the tiny helpers that read and parse the
// per-project "files every agent must read first" list. The list
// itself lives in the existing bbolt settings store under the
// project bucket; the helpers here keep prompt builders, the CLI,
// and the workflow layer decoupled from that storage detail.
//
// Deliberately minimal: no validation, no file-existence checks,
// case preserved verbatim throughout. `AGENTS.md` and `agents.md`
// are different paths on case-sensitive filesystems and the user's
// raw input must reach the prompt without rewriting.
package mustread

import (
	"fmt"
	"strings"

	"github.com/spacelions/j/internal/store"
)

// Key is the bbolt key under store.BucketProject that holds the
// `;`-separated must-read list.
const Key = "mustread"

// Load wraps store.Get so callers don't need to remember the bucket
// or key. The boolean is false when the key has never been set
// (preflight will then prompt for it); an explicit empty string is
// reported as ("", true, nil).
func Load(s *store.Store) (string, bool, error) {
	return s.Get(store.BucketProject, Key)
}

// LoadFromDefault opens the per-project settings store, reads the
// mustread value, and returns it parsed into a list of files. It is
// the single helper the CLI uses to seed the per-run must-read list
// before dispatching to claude / cursor; preflight has already
// captured the value, so the lookup is read-only.
//
// On any failure (store can't be opened, value can't be read) it
// returns nil plus a wrapped error so callers can log via their
// existing stderr-warning pattern and pass nil through to the
// agent — the workflow must never block on a mustread lookup.
func LoadFromDefault() ([]string, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("mustread: default path: %w", err)
	}
	s, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("mustread: open store: %w", err)
	}
	defer s.Close()
	value, set, err := Load(s)
	if err != nil {
		return nil, fmt.Errorf("mustread: load: %w", err)
	}
	if !set {
		return nil, nil
	}
	return Parse(value), nil
}

// Parse splits a must-read setting value into its individual file
// entries. The separator is `;`; surrounding whitespace is trimmed
// and empty fragments are dropped. Case is preserved verbatim. An
// empty or whitespace-only input returns nil so callers can pass the
// result straight into the prompt builders' "no must-read block"
// branch.
func Parse(value string) []string {
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
